package cloudplane

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net"

	cloudplaneapi "mini-cloud/internal/cloudplane/api"
	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	cloudplanecontrol "mini-cloud/internal/cloudplane/control"
	cloudplaneingress "mini-cloud/internal/cloudplane/control/ingress"
	caddyingress "mini-cloud/internal/cloudplane/infra/ingress/caddy"
	runtimepoolcloud "mini-cloud/internal/cloudplane/infra/runtimepool/cloud"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudplanemigrations "mini-cloud/internal/cloudplane/infra/store/migrations"
	"mini-cloud/internal/common/util"

	"google.golang.org/grpc"
)

// ErrConfigRequired 表示 cloud-plane CLI 没有收到配置文件路径。
var ErrConfigRequired = errors.New("config is required")

// RunCLI 解析 cloud-plane 命令行参数，组装内部 gRPC 与后台 reconciler，并阻塞运行到 ctx 取消或服务出错。
// 参数说明：ctx 控制 cloud-plane 进程退出；logger 记录进程级运行日志；args 是不含程序名的命令行参数；stderr 用于输出用法和参数错误。
func RunCLI(ctx context.Context, logger *slog.Logger, args []string, stderr io.Writer) error {
	// 没有任何参数时直接打印用法；cloud-plane 不提供隐式默认配置路径，避免误启动到错误环境。
	if len(args) == 0 {
		PrintUsage(stderr)
		return ErrConfigRequired
	}
	// 兼容常见的 help 子命令形式，但只返回 flag.ErrHelp，不继续初始化任何运行时依赖。
	if len(args) == 1 && args[0] == "help" {
		PrintUsage(stderr)
		return flag.ErrHelp
	}

	// 使用独立 FlagSet，避免污染全局 flag，也方便测试直接传入 args。
	fs := flag.NewFlagSet("cloud-plane", flag.ContinueOnError)
	// 把 flag 包的错误和用法输出写到调用方传入的 stderr，保持 CLI 输出可测试。
	fs.SetOutput(stderr)
	// 统一由 PrintUsage 输出帮助文本，避免 flag 包生成与项目约定不一致的默认帮助。
	fs.Usage = func() {
		PrintUsage(stderr)
	}
	// cloud-plane 只接受一个显式配置文件路径，所有进程配置都从 YAML 读取。
	configPath := fs.String("config", "", "path to cloud-plane YAML config file")
	// 解析失败时直接返回 flag 包错误；这里不包装，保留调用方对 flag.ErrHelp 等错误的判断能力。
	if err := fs.Parse(args); err != nil {
		return err
	}
	// 解析后仍有位置参数说明调用方式不符合约定，打印用法并按 help 类错误返回。
	if fs.NArg() > 0 {
		PrintUsage(stderr)
		return flag.ErrHelp
	}
	// 配置路径必须显式传入；不从环境变量或固定路径兜底，保证启动来源清晰。
	if *configPath == "" {
		return ErrConfigRequired
	}

	// 读取并校验 cloud-plane YAML 配置，同时完成默认值和派生配置的归一化。
	cfg, err := cloudplaneconfig.Load(*configPath)
	if err != nil {
		return err
	}

	// 根据配置中的云厂商运行时参数创建 runtime driver，用于创建和盘点 runtime node 云资源。
	driver, err := runtimepoolcloud.NewRuntimeDriver(cfg.ToProviderRuntimeConfig())
	if err != nil {
		return err
	}

	// 打开 cloud-plane 本地数据库；数据库是 execution、node、runtime 状态和同步视图的本地事实源。
	db, err := store.Open(cfg.Database.URL)
	if err != nil {
		return err
	}
	// 启动前先迁移 schema，确保后续 store 和 gRPC 服务看到的是当前代码期望的数据结构。
	if err := cloudplanemigrations.Up(db); err != nil {
		// 迁移失败时主动关闭数据库；如果关闭也失败，把两个错误一起返回，避免丢失资源释放问题。
		if closeErr := db.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	// 正常退出路径统一关闭数据库；关闭失败只记录日志，不覆盖 RunCLI 的主流程返回结果。
	defer func() {
		if err := db.Close(); err != nil {
			logger.Warn("close cloud-plane database failed", "error", err)
		}
	}()

	// 给进程级 logger 增加稳定上下文字段，后续 reconciler、gRPC handler 日志都能定位 plane 和地域。
	logger = logger.With(
		"plane_name", cfg.Plane.Identity.Name,
		"provider", cfg.Infrastructure.Provider,
		"region", cfg.Infrastructure.Location.RegionID,
	)
	// Store 聚合所有数据库访问方法，供 gRPC 服务和 reconciler 共享同一份本地状态。
	stores := store.New(db)
	// ingressController 只生成路由快照；Caddyfile 渲染、文件写入和 reload 由 infra sink 完成。
	ingressController := cloudplaneingress.NewController(logger, stores, cfg, caddyingress.NewSink(logger, caddyingress.Config{
		ListenHTTPAddr: cfg.Ingress.Caddy.ListenHTTPAddr,
		ConfigPath:     cfg.Ingress.Caddy.ConfigPath,
		ReloadCommand:  cfg.Ingress.Caddy.ReloadCommand,
	}))
	// reconcilerManager 负责 cloud-plane 本地后台收敛循环，例如 node 心跳巡检、ingress 发布和 runtime node 缩容。
	reconcilerManager := cloudplanecontrol.NewManager(logger, stores, driver, ingressController)
	// gRPC server 是 cloud-plane 对 control-plane 和 node-agent 暴露的唯一进程入口。
	grpcServer := cloudplaneapi.NewGRPCServer(cloudplaneapi.Options{
		Config:        cfg,
		RuntimeDriver: driver,
	}, logger, db, stores)
	// RunCLI 返回时兜底 Stop gRPC server；正常 ctx 退出路径会先尝试 GracefulStop。
	defer grpcServer.Stop()

	// 记录启动参数，便于从日志确认实际加载的配置路径、监听地址和地域。
	logger.Info("starting mini-cloud cloud-plane",
		"config_path", cfg.Path,
		"grpc_addr", cfg.Server.ListenGRPCAddr,
		"zone", cfg.Infrastructure.Location.ZoneID,
	)

	// 创建 TCP listener；只有 listener 成功后才启动 gRPC Serve。
	listener, err := net.Listen("tcp", cfg.Server.ListenGRPCAddr)
	if err != nil {
		return err
	}
	// 确保异常返回时释放监听端口；正常 GracefulStop 后关闭 listener 也是安全的。
	defer listener.Close()

	// runCtx 只控制 cloud-plane 内部后台任务；外层 ctx 取消时会显式 cancelRun。
	runCtx, cancelRun := context.WithCancel(ctx)
	// 启动后台 reconciler；此时 listener 已创建成功，后续 gRPC Serve 会在单独 goroutine 中运行。
	reconcilerManager.Start(runCtx)
	// 先注册 Wait，再注册 cancelRun；函数返回时会先取消后台任务，再等待后台任务退出。
	defer reconcilerManager.Wait()
	defer cancelRun()

	// errCh 接收 gRPC Serve 返回的非 nil error；其中 server 停止类错误在退出路径中按预期处理。
	errCh := make(chan error, 1)
	go func() {
		// Serve 阻塞处理 gRPC 连接；当 listener accept 出错或 server 被 Stop/GracefulStop 停止时返回。
		if err := grpcServer.Serve(listener); err != nil {
			errCh <- err
		}
	}()

	// 主 goroutine 等待两类事件：gRPC server 出错，或外层 ctx 要求进程退出。
	select {
	case runErr := <-errCh:
		// Serve 提前返回时先停止后台 reconciler，再把返回错误交给调用方。
		cancelRun()
		return runErr
	case <-ctx.Done():
		// 收到进程退出信号时先通知后台任务停止，避免它继续发起新工作。
		cancelRun()
		// 优雅停止 gRPC：停止接收新连接，并等待已建立 RPC 尽量完成；该调用可能阻塞到现有 RPC 结束。
		grpcServer.GracefulStop()
		// GracefulStop 可能让 Serve 返回；这里非阻塞读取一次，区分预期停止和异常错误。
		select {
		case runErr := <-errCh:
			// listener 关闭和 gRPC server stopped 都属于预期退出路径；其他错误仍返回给调用方。
			if runErr != nil && !errors.Is(runErr, net.ErrClosed) && !errors.Is(runErr, grpc.ErrServerStopped) {
				return runErr
			}
		default:
			// 如果此刻还没有读到 Serve 返回值，不在这里额外阻塞等待。
		}
		return nil
	}
}

// PrintUsage 向 stderr 写入 cloud-plane 命令行用法。
// 参数说明：stderr 是命令行帮助文本的输出目标。
func PrintUsage(stderr io.Writer) {
	util.Fprintln(stderr, "usage:")
	util.Fprintln(stderr, "  cloud-plane --config ./cloud-plane.yaml")
}
