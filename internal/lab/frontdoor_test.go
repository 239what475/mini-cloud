package lab

import "testing"

func TestDNSPodRecordHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		root string
		want string
	}{
		{name: "demo.apps", root: "whatcloud.cn", want: "demo.apps.whatcloud.cn"},
		{name: "@", root: "whatcloud.cn", want: "whatcloud.cn"},
		{name: "", root: "whatcloud.cn", want: "whatcloud.cn"},
	}

	for _, tt := range tests {
		if got := dnsPodRecordHost(tt.name, tt.root); got != tt.want {
			t.Fatalf("dnsPodRecordHost(%q, %q) = %q, want %q", tt.name, tt.root, got, tt.want)
		}
	}
}

func TestLabDomainIsUnder(t *testing.T) {
	t.Parallel()

	if !labDomainIsUnder("demo.apps.whatcloud.cn", "apps.whatcloud.cn") {
		t.Fatal("expected service host to be under base domain")
	}
	if labDomainIsUnder("tx-origin.whatcloud.cn", "apps.whatcloud.cn") {
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
