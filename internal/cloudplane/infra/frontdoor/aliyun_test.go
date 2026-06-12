package frontdoor

import (
	"context"
	"errors"
	"fmt"
	"testing"

	cdn20180510 "github.com/alibabacloud-go/cdn-20180510/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
)

func TestAliyunPrepareDomainWritesOwnerVerifyTXT(t *testing.T) {
	t.Parallel()

	api := &fakeAliyunCDN{
		verifyContent: map[string]interface{}{
			"RootDomain": "whatcloud.cn",
			"verifyCode": "verify_test",
			"verifyKey":  "verification",
		},
	}
	dns := &fakeDNS{records: map[string]DNSRecord{}}
	client := &aliyunCDNClient{client: api, rawClient: api, origin: "203.0.113.10"}

	verification, err := client.PrepareDomain(context.Background(), "demo.apps.whatcloud.cn", dns)
	if err != nil {
		t.Fatalf("PrepareDomain returned error: %v", err)
	}
	if verification == nil || verification.Subdomain != "verification.whatcloud.cn" || verification.Type != "TXT" || verification.Value != "verify_test" {
		t.Fatalf("verification = %+v", verification)
	}
	record := dns.records["verification.whatcloud.cn"]
	if record.Type != "TXT" || record.Value != "verify_test" {
		t.Fatalf("verify record = %+v", record)
	}
	if api.verifiedDomain != "demo.apps.whatcloud.cn" {
		t.Fatalf("verified domain = %q", api.verifiedDomain)
	}
}

func TestAliyunPrepareDomainReturnsPendingWhenOwnerVerificationWaits(t *testing.T) {
	t.Parallel()

	api := &fakeAliyunCDN{
		verifyContent: map[string]interface{}{
			"RootDomain": "whatcloud.cn",
			"verifyCode": "verify_test",
			"verifyKey":  "verification",
		},
		verifyErr: fmt.Errorf("DomainOwnerVerifyFail: owner verification pending"),
	}
	dns := &fakeDNS{records: map[string]DNSRecord{}}
	client := &aliyunCDNClient{client: api, rawClient: api, origin: "203.0.113.10"}

	verification, err := client.PrepareDomain(context.Background(), "demo.apps.whatcloud.cn", dns)
	if !errors.Is(err, errDomainVerificationPending) {
		t.Fatalf("PrepareDomain error = %v, want errDomainVerificationPending", err)
	}
	if verification == nil || verification.Subdomain != "verification.whatcloud.cn" {
		t.Fatalf("verification = %+v", verification)
	}
	if dns.records["verification.whatcloud.cn"].Value != "verify_test" {
		t.Fatalf("verify record = %+v", dns.records)
	}
}

type fakeAliyunCDN struct {
	verifyContent  map[string]interface{}
	verifiedDomain string
	verifyErr      error
}

func (f *fakeAliyunCDN) AddCdnDomain(*cdn20180510.AddCdnDomainRequest) (*cdn20180510.AddCdnDomainResponse, error) {
	return nil, nil
}

func (f *fakeAliyunCDN) BatchSetCdnDomainConfig(*cdn20180510.BatchSetCdnDomainConfigRequest) (*cdn20180510.BatchSetCdnDomainConfigResponse, error) {
	return nil, nil
}

func (f *fakeAliyunCDN) CallApi(*openapi.Params, *openapi.OpenApiRequest, *util.RuntimeOptions) (map[string]interface{}, error) {
	return map[string]interface{}{
		"body": map[string]interface{}{
			"Content": f.verifyContent,
		},
	}, nil
}

func (f *fakeAliyunCDN) DescribeUserDomains(*cdn20180510.DescribeUserDomainsRequest) (*cdn20180510.DescribeUserDomainsResponse, error) {
	return &cdn20180510.DescribeUserDomainsResponse{Body: &cdn20180510.DescribeUserDomainsResponseBody{}}, nil
}

func (f *fakeAliyunCDN) StopCdnDomain(*cdn20180510.StopCdnDomainRequest) (*cdn20180510.StopCdnDomainResponse, error) {
	return nil, nil
}

func (f *fakeAliyunCDN) DeleteCdnDomain(*cdn20180510.DeleteCdnDomainRequest) (*cdn20180510.DeleteCdnDomainResponse, error) {
	return nil, nil
}

func (f *fakeAliyunCDN) VerifyDomainOwner(req *cdn20180510.VerifyDomainOwnerRequest) (*cdn20180510.VerifyDomainOwnerResponse, error) {
	if req != nil && req.DomainName != nil {
		f.verifiedDomain = *req.DomainName
	}
	return nil, f.verifyErr
}
