package cloudplaneapi

import "testing"

// TestProjectConfigSetValidate 验证 config set 期望状态校验规则。
func TestProjectConfigSetValidate(t *testing.T) {
	tests := []struct {
		name    string
		item    ProjectConfigSet
		wantErr error
	}{
		{
			name: "valid",
			item: ProjectConfigSet{
				ID:     "cfg-web-config",
				Name:   "web-config",
				Values: map[string]string{"SERVICE_MODE": "prod"},
			},
		},
		{
			name:    "id required",
			item:    ProjectConfigSet{Name: "web-config", Values: map[string]string{"A": "1"}},
			wantErr: ErrConfigSetIDRequired,
		},
		{
			name:    "name required",
			item:    ProjectConfigSet{ID: "cfg-web-config", Values: map[string]string{"A": "1"}},
			wantErr: ErrConfigSetNameRequired,
		},
		{
			name:    "invalid name",
			item:    ProjectConfigSet{ID: "cfg-web-config", Name: "Web Config", Values: map[string]string{"A": "1"}},
			wantErr: ErrConfigSetNameInvalid,
		},
		{
			name:    "values required",
			item:    ProjectConfigSet{ID: "cfg-web-config", Name: "web-config"},
			wantErr: ErrConfigSetValuesRequired,
		},
		{
			name:    "key required",
			item:    ProjectConfigSet{ID: "cfg-web-config", Name: "web-config", Values: map[string]string{"": "1"}},
			wantErr: ErrConfigSetValueKeyInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.item.Validate()
			if tt.wantErr == nil && err != nil {
				t.Fatalf("ProjectConfigSet.Validate() unexpected error = %v", err)
			}
			if tt.wantErr != nil && err != tt.wantErr {
				t.Fatalf("ProjectConfigSet.Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestProjectSecretSetValidateRejectsEmptyValues 验证 secret set 不能为空。
func TestProjectSecretSetValidateRejectsEmptyValues(t *testing.T) {
	t.Parallel()

	err := ProjectSecretSet{ID: "sec-service-secrets", Name: "service-secrets", Values: map[string]string{}}.Validate()
	if err != ErrSecretSetValuesRequired {
		t.Fatalf("expected ErrSecretSetValuesRequired, got %v", err)
	}
}

// TestProjectRegistryCredentialValidateRejectsMissingPassword 验证 registry credential 必须包含密码。
func TestProjectRegistryCredentialValidateRejectsMissingPassword(t *testing.T) {
	t.Parallel()

	err := ProjectRegistryCredential{
		ID:       "reg-harbor-main",
		Name:     "harbor-main",
		Server:   "registry.example.com",
		Username: "student",
	}.Validate()
	if err != ErrRegistryCredentialPasswordRequired {
		t.Fatalf("expected ErrRegistryCredentialPasswordRequired, got %v", err)
	}
}
