package operatorapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"mini-cloud/internal/controlplane/planesync"
	"mini-cloud/internal/controlplane/servicecontroller"
	"mini-cloud/internal/controlplane/store"
	controlplanev1 "mini-cloud/internal/gen/proto/minicloud/controlplane/v1"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

type TransportSet struct {
	GRPC        http.Handler
	GatewayHTTP http.Handler
}

func NewTransportSet(
	logger *slog.Logger,
	stores *store.Store,
	adminToken string,
	planeSync *planesync.Service,
	serviceControl *servicecontroller.Controller,
) (TransportSet, error) {
	service := NewService(logger, stores, adminToken, planeSync, serviceControl)

	grpcServer := grpc.NewServer()
	controlplanev1.RegisterOperatorServiceServer(grpcServer, service)

	gateway := newGatewayServeMux()
	if err := controlplanev1.RegisterOperatorServiceHandlerServer(context.Background(), gateway, service); err != nil {
		return TransportSet{}, err
	}

	return TransportSet{
		GRPC:        grpcServer,
		GatewayHTTP: gateway,
	}, nil
}

func IsGRPCRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.ProtoMajor != 2 {
		return false
	}
	if r.Method == "PRI" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/grpc")
}

func newGatewayServeMux() *runtime.ServeMux {
	return runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				EmitUnpopulated: true,
			},
		}),
		runtime.WithIncomingHeaderMatcher(operatorIncomingHeaderMatcher),
		runtime.WithErrorHandler(operatorHTTPErrorHandler),
		runtime.WithRoutingErrorHandler(operatorGatewayRoutingErrorHandler),
	)
}

func operatorIncomingHeaderMatcher(key string) (string, bool) {
	switch strings.ToLower(key) {
	case "authorization":
		return "authorization", true
	default:
		return runtime.DefaultHeaderMatcher(key)
	}
}

func operatorHTTPErrorHandler(_ context.Context, _ *runtime.ServeMux, _ runtime.Marshaler, w http.ResponseWriter, _ *http.Request, err error) {
	st := grpcstatus.Convert(err)
	statusCode := runtime.HTTPStatusFromCode(st.Code())
	if statusCode < 400 {
		statusCode = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": st.Message()})
}

func operatorGatewayRoutingErrorHandler(_ context.Context, _ *runtime.ServeMux, _ runtime.Marshaler, w http.ResponseWriter, _ *http.Request, statusCode int) {
	message := http.StatusText(statusCode)
	if message == "" {
		message = "request failed"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message})
}
