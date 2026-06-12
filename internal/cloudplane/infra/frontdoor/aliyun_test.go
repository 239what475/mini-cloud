package frontdoor

import (
	"context"
	"errors"
	"fmt"
	"testing"

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
	client := &aliyunCDNClient{rawClient: api, origin: "203.0.113.10"}

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
	client := &aliyunCDNClient{rawClient: api, origin: "203.0.113.10"}

	verification, err := client.PrepareDomain(context.Background(), "demo.apps.whatcloud.cn")
	if !errors.Is(err, errDomainVerificationPending) {
		t.Fatalf("PrepareDomain error = %v, want errDomainVerificationPending", err)
	}
	if verification == nil || verification.Subdomain != "verification.whatcloud.cn" {
		t.Fatalf("verification = %+v", verification)
	}
}

func TestAliyunEnsureDomainReadsExistingCNAME(t *testing.T) {
	t.Parallel()

	api := &fakeAliyunCDN{
		domainPageData: []interface{}{
			map[string]interface{}{
				"DomainName": "demo.apps.whatcloud.cn",
				"Cname":      "demo.apps.whatcloud.cn.w.kunlunsl.com",
			},
		},
	}
	client := &aliyunCDNClient{rawClient: api, origin: "203.0.113.10"}

	cname, err := client.EnsureDomain(context.Background(), "demo.apps.whatcloud.cn")
	if err != nil {
		t.Fatalf("EnsureDomain returned error: %v", err)
	}
	if cname != "demo.apps.whatcloud.cn.w.kunlunsl.com" {
		t.Fatalf("cname = %q", cname)
	}
	if api.setOriginHost != "demo.apps.whatcloud.cn" {
		t.Fatalf("set origin host = %q", api.setOriginHost)
	}
}

type fakeAliyunCDN struct {
	domainPageData []interface{}
	verifyContent  map[string]interface{}
	verifiedDomain string
	setOriginHost  string
	verifyErr      error
}

func (f *fakeAliyunCDN) CallApiWithCtx(_ context.Context, params *openapi.Params, req *openapi.OpenApiRequest, _ *util.RuntimeOptions) (map[string]interface{}, error) {
	action := ""
	if params != nil && params.Action != nil {
		action = *params.Action
	}
	switch action {
	case "DescribeUserDomains":
		return map[string]interface{}{
			"body": map[string]interface{}{
				"Domains": map[string]interface{}{"PageData": f.domainPageData},
			},
		}, nil
	case "DescribeDomainVerifyData":
		return map[string]interface{}{
			"body": map[string]interface{}{
				"Content": f.verifyContent,
			},
		}, nil
	case "VerifyDomainOwner":
		if req != nil && req.Query != nil && req.Query["DomainName"] != nil {
			f.verifiedDomain = *req.Query["DomainName"]
		}
		return map[string]interface{}{}, f.verifyErr
	case "BatchSetCdnDomainConfig":
		if req != nil && req.Query != nil && req.Query["DomainNames"] != nil {
			f.setOriginHost = *req.Query["DomainNames"]
		}
		return map[string]interface{}{}, nil
	default:
		return map[string]interface{}{}, nil
	}
}
