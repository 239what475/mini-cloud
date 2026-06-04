package cloudplaneapi

import "testing"

// TestConfigSetValidate 验证 config set 期望状态校验规则。
func TestConfigSetValidate(t *testing.T) {
	tests := []struct {
		name    string
		item    ConfigSet
		wantErr error
	}{
		{
			name: "valid",
			item: ConfigSet{
				ID:     "cfg-web-config",
				Name:   "web-config",
				Values: map[string]string{"SERVICE_MODE": "prod"},
			},
		},
		{
			name:    "id required",
			item:    ConfigSet{Name: "web-config", Values: map[string]string{"A": "1"}},
			wantErr: ErrConfigSetIDRequired,
		},
		{
			name:    "name required",
			item:    ConfigSet{ID: "cfg-web-config", Values: map[string]string{"A": "1"}},
			wantErr: ErrConfigSetNameRequired,
		},
		{
			name:    "invalid name",
			item:    ConfigSet{ID: "cfg-web-config", Name: "Web Config", Values: map[string]string{"A": "1"}},
			wantErr: ErrConfigSetNameInvalid,
		},
		{
			name:    "values required",
			item:    ConfigSet{ID: "cfg-web-config", Name: "web-config"},
			wantErr: ErrConfigSetValuesRequired,
		},
		{
			name:    "key required",
			item:    ConfigSet{ID: "cfg-web-config", Name: "web-config", Values: map[string]string{"": "1"}},
			wantErr: ErrConfigSetValueKeyInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.item.Validate()
			if tt.wantErr == nil && err != nil {
				t.Fatalf("ConfigSet.Validate() unexpected error = %v", err)
			}
			if tt.wantErr != nil && err != tt.wantErr {
				t.Fatalf("ConfigSet.Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestSecretSetValidateRejectsEmptyValues 验证 secret set 不能为空。
func TestSecretSetValidateRejectsEmptyValues(t *testing.T) {
	t.Parallel()

	err := SecretSet{ID: "sec-service-secrets", Name: "service-secrets", Values: map[string]string{}}.Validate()
	if err != ErrSecretSetValuesRequired {
		t.Fatalf("expected ErrSecretSetValuesRequired, got %v", err)
	}
}

// TestRegistryCredentialValidateRejectsMissingPassword 验证 registry credential 必须包含密码。
func TestRegistryCredentialValidateRejectsMissingPassword(t *testing.T) {
	t.Parallel()

	err := RegistryCredential{
		ID:       "reg-harbor-main",
		Name:     "harbor-main",
		Server:   "registry.example.com",
		Username: "student",
	}.Validate()
	if err != ErrRegistryCredentialPasswordRequired {
		t.Fatalf("expected ErrRegistryCredentialPasswordRequired, got %v", err)
	}
}
