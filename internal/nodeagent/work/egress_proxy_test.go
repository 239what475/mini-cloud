package work

import "testing"

// TestInjectEgressProxyEnvAddsUpperAndLowerCaseVariables 验证 workload 出公网代理同时注入大小写环境变量。
func TestInjectEgressProxyEnvAddsUpperAndLowerCaseVariables(t *testing.T) {
	env := injectEgressProxyEnv(map[string]string{"APP_ENV": "prod"}, Options{
		EgressProxyEnabled:  true,
		EgressProxyEndpoint: "http://10.0.0.10:3128",
		EgressProxyNoProxy:  []string{"127.0.0.1", "localhost"},
	})
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		if env[key] != "http://10.0.0.10:3128" {
			t.Fatalf("%s = %q", key, env[key])
		}
	}
	if env["NO_PROXY"] != "127.0.0.1,localhost" || env["no_proxy"] != "127.0.0.1,localhost" {
		t.Fatalf("unexpected no_proxy values: %+v", env)
	}
	if env["APP_ENV"] != "prod" {
		t.Fatalf("existing env was not preserved: %+v", env)
	}
}
