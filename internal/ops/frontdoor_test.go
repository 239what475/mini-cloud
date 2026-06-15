package ops

import (
	"errors"
	"testing"
)

func TestDomainIsUnder(t *testing.T) {
	t.Parallel()

	if !domainIsUnder("demo.apps.example.com", "apps.example.com") {
		t.Fatal("expected service host to be under base domain")
	}
	if domainIsUnder("origin.example.com", "apps.example.com") {
		t.Fatal("origin host must not be treated as a managed service host")
	}
}

func TestProviderOwnsCNAME(t *testing.T) {
	t.Parallel()

	if !providerOwnsCNAME("aliyun", "demo.w.kunlunaq.com.") {
		t.Fatal("expected Aliyun CNAME ownership")
	}
	if providerOwnsCNAME("aliyun", "demo.cdn.dnsv1.com.") {
		t.Fatal("Aliyun must not own Tencent CDN CNAME")
	}
	if !providerOwnsCNAME("tencent", "demo.cdn.dnsv1.com.") {
		t.Fatal("expected Tencent CNAME ownership")
	}
	if providerOwnsCNAME("tencent", "demo.w.kunlunaq.com.") {
		t.Fatal("Tencent must not own Aliyun CDN CNAME")
	}
}

func TestSCFCustomDomainCNAMETarget(t *testing.T) {
	t.Parallel()

	got := scfCustomDomainCNAMETarget("https://1419114191-k51nqyutmf.ap-guangzhou.tencentscf.com")
	if got != "1419114191.ap-guangzhou.tencentscf.com" {
		t.Fatalf("scfCustomDomainCNAMETarget = %q", got)
	}
}

func TestSCFCustomDomainCNAMEPending(t *testing.T) {
	t.Parallel()

	err := errors.New("[TencentCloudSDKException] code:FailedOperation.CNAME message:域名必须先添加CNAME记录")
	if !scfCustomDomainCNAMEPending(err) {
		t.Fatal("expected CNAME validation error to be retryable")
	}
	if scfCustomDomainCNAMEPending(errors.New("AuthFailure: not authorized")) {
		t.Fatal("authorization errors must not be retryable")
	}
}

func TestCommandOutputIndicatesMissingResourceDoesNotHideAuthorizationErrors(t *testing.T) {
	t.Parallel()

	if !commandOutputIndicatesMissingResource(errors.New("ResourceNotFound: domain does not exist")) {
		t.Fatal("expected missing resource error")
	}
	if !commandOutputIndicatesMissingResource(errors.New("FailedOperation.Domain.UnExist: 该域名未找到")) {
		t.Fatal("expected Tencent unexist domain error")
	}
	if commandOutputIndicatesMissingResource(errors.New("not authorized to delete domain")) {
		t.Fatal("authorization errors must not be treated as missing resources")
	}
}
