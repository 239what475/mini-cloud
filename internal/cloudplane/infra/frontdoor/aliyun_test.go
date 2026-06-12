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

func TestAliyunPrepareDomainReturnsOwnerVerifyTXT(t *testing.T) {
	t.Parallel()

	api := &fakeAliyunCDN{
		verifyContent: map[string]interface{}{
			"RootDomain": "whatcloud.cn",
			"verifyCode": "verify_test",
			"verifyKey":  "verification",
		},
	}
	client := &aliyunCDNClient{client: api, rawClient: api, origin: "203.0.113.10"}

	verification, err := client.PrepareDomain(context.Background(), "demo.apps.whatcloud.cn")
	if err != nil {
		t.Fatalf("PrepareDomain returned error: %v", err)
	}
	if verification == nil || verification.Subdomain != "verification.whatcloud.cn" || verification.Type != "TXT" || verification.Value != "verify_test" {
		t.Fatalf("verification = %+v", verification)
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
	client := &aliyunCDNClient{client: api, rawClient: api, origin: "203.0.113.10"}

	verification, err := client.PrepareDomain(context.Background(), "demo.apps.whatcloud.cn")
	if !errors.Is(err, errDomainVerificationPending) {
		t.Fatalf("PrepareDomain error = %v, want errDomainVerificationPending", err)
	}
	if verification == nil || verification.Subdomain != "verification.whatcloud.cn" {
		t.Fatalf("verification = %+v", verification)
	}
}

type fakeAliyunCDN struct {
	verifyContent  map[string]interface{}
	verifiedDomain string
	verifyErr      error
}

func (f *fakeAliyunCDN) AddCdnDomainWithOptions(*cdn20180510.AddCdnDomainRequest, *util.RuntimeOptions) (*cdn20180510.AddCdnDomainResponse, error) {
	return nil, nil
}

func (f *fakeAliyunCDN) BatchSetCdnDomainConfigWithOptions(*cdn20180510.BatchSetCdnDomainConfigRequest, *util.RuntimeOptions) (*cdn20180510.BatchSetCdnDomainConfigResponse, error) {
	return nil, nil
}

func (f *fakeAliyunCDN) CallApiWithCtx(context.Context, *openapi.Params, *openapi.OpenApiRequest, *util.RuntimeOptions) (map[string]interface{}, error) {
	return map[string]interface{}{
		"body": map[string]interface{}{
			"Content": f.verifyContent,
		},
	}, nil
}

func (f *fakeAliyunCDN) DescribeUserDomainsWithOptions(*cdn20180510.DescribeUserDomainsRequest, *util.RuntimeOptions) (*cdn20180510.DescribeUserDomainsResponse, error) {
	return &cdn20180510.DescribeUserDomainsResponse{Body: &cdn20180510.DescribeUserDomainsResponseBody{}}, nil
}

func (f *fakeAliyunCDN) StopCdnDomainWithOptions(*cdn20180510.StopCdnDomainRequest, *util.RuntimeOptions) (*cdn20180510.StopCdnDomainResponse, error) {
	return nil, nil
}

func (f *fakeAliyunCDN) DeleteCdnDomainWithOptions(*cdn20180510.DeleteCdnDomainRequest, *util.RuntimeOptions) (*cdn20180510.DeleteCdnDomainResponse, error) {
	return nil, nil
}

func (f *fakeAliyunCDN) VerifyDomainOwnerWithOptions(req *cdn20180510.VerifyDomainOwnerRequest, _ *util.RuntimeOptions) (*cdn20180510.VerifyDomainOwnerResponse, error) {
	if req != nil && req.DomainName != nil {
		f.verifiedDomain = *req.DomainName
	}
	return nil, f.verifyErr
}
