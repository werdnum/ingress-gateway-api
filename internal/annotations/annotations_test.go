package annotations

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestGetString(t *testing.T) {
	tests := []struct {
		name      string
		annots    map[string]string
		key       string
		wantValue string
		wantOK    bool
	}{
		{
			name:      "existing key",
			annots:    map[string]string{"foo": "bar"},
			key:       "foo",
			wantValue: "bar",
			wantOK:    true,
		},
		{
			name:      "missing key",
			annots:    map[string]string{"foo": "bar"},
			key:       "baz",
			wantValue: "",
			wantOK:    false,
		},
		{
			name:      "nil annotations",
			annots:    nil,
			key:       "foo",
			wantValue: "",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewAnnotationSet(tt.annots)
			got, ok := as.GetString(tt.key)
			if ok != tt.wantOK {
				t.Errorf("GetString() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.wantValue {
				t.Errorf("GetString() = %v, want %v", got, tt.wantValue)
			}
		})
	}
}

func TestGetDuration(t *testing.T) {
	tests := []struct {
		name       string
		annots     map[string]string
		key        string
		wantString string
		wantOK     bool
	}{
		{
			name:       "seconds as integer",
			annots:     map[string]string{ProxyReadTimeout: "30"},
			key:        ProxyReadTimeout,
			wantString: "30s",
			wantOK:     true,
		},
		{
			name:       "duration string",
			annots:     map[string]string{ProxyReadTimeout: "1m30s"},
			key:        ProxyReadTimeout,
			wantString: "1m30s",
			wantOK:     true,
		},
		{
			name:       "invalid duration",
			annots:     map[string]string{ProxyReadTimeout: "invalid"},
			key:        ProxyReadTimeout,
			wantString: "",
			wantOK:     false,
		},
		{
			name:       "missing key",
			annots:     map[string]string{},
			key:        ProxyReadTimeout,
			wantString: "",
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewAnnotationSet(tt.annots)
			got, ok := as.GetDuration(tt.key)
			if ok != tt.wantOK {
				t.Errorf("GetDuration() ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantOK && got != nil && string(*got) != tt.wantString {
				t.Errorf("GetDuration() = %v, want %v", string(*got), tt.wantString)
			}
		})
	}
}

func TestGetQuantity(t *testing.T) {
	tests := []struct {
		name       string
		annots     map[string]string
		key        string
		wantValue  string
		wantOK     bool
	}{
		{
			name:       "nginx format lowercase k",
			annots:     map[string]string{ProxyBufferSize: "4k"},
			key:        ProxyBufferSize,
			wantValue:  "4Ki",
			wantOK:     true,
		},
		{
			name:       "nginx format lowercase m",
			annots:     map[string]string{ProxyBodySize: "16m"},
			key:        ProxyBodySize,
			wantValue:  "16Mi",
			wantOK:     true,
		},
		{
			name:       "kubernetes format",
			annots:     map[string]string{ProxyBufferSize: "4Ki"},
			key:        ProxyBufferSize,
			wantValue:  "4Ki",
			wantOK:     true,
		},
		{
			name:       "plain number",
			annots:     map[string]string{ProxyBufferSize: "4096"},
			key:        ProxyBufferSize,
			wantValue:  "4096",
			wantOK:     true,
		},
		{
			name:       "invalid value",
			annots:     map[string]string{ProxyBufferSize: "invalid"},
			key:        ProxyBufferSize,
			wantValue:  "",
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewAnnotationSet(tt.annots)
			got, ok := as.GetQuantity(tt.key)
			if ok != tt.wantOK {
				t.Errorf("GetQuantity() ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantOK && got != nil {
				expected := resource.MustParse(tt.wantValue)
				if !got.Equal(expected) {
					t.Errorf("GetQuantity() = %v, want %v", got.String(), expected.String())
				}
			}
		})
	}
}

func TestGetBool(t *testing.T) {
	tests := []struct {
		name      string
		annots    map[string]string
		key       string
		wantValue bool
		wantOK    bool
	}{
		{
			name:      "true",
			annots:    map[string]string{SSLRedirect: "true"},
			key:       SSLRedirect,
			wantValue: true,
			wantOK:    true,
		},
		{
			name:      "false",
			annots:    map[string]string{SSLRedirect: "false"},
			key:       SSLRedirect,
			wantValue: false,
			wantOK:    true,
		},
		{
			name:      "1",
			annots:    map[string]string{SSLRedirect: "1"},
			key:       SSLRedirect,
			wantValue: true,
			wantOK:    true,
		},
		{
			name:      "invalid",
			annots:    map[string]string{SSLRedirect: "invalid"},
			key:       SSLRedirect,
			wantValue: false,
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewAnnotationSet(tt.annots)
			got, ok := as.GetBool(tt.key)
			if ok != tt.wantOK {
				t.Errorf("GetBool() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.wantValue {
				t.Errorf("GetBool() = %v, want %v", got, tt.wantValue)
			}
		})
	}
}

func TestGetStringSlice(t *testing.T) {
	tests := []struct {
		name      string
		annots    map[string]string
		key       string
		wantValue []string
		wantOK    bool
	}{
		{
			name:      "single value",
			annots:    map[string]string{CORSAllowOrigin: "https://example.com"},
			key:       CORSAllowOrigin,
			wantValue: []string{"https://example.com"},
			wantOK:    true,
		},
		{
			name:      "multiple values",
			annots:    map[string]string{CORSAllowOrigin: "https://example.com, https://other.com"},
			key:       CORSAllowOrigin,
			wantValue: []string{"https://example.com", "https://other.com"},
			wantOK:    true,
		},
		{
			name:      "empty value",
			annots:    map[string]string{CORSAllowOrigin: ""},
			key:       CORSAllowOrigin,
			wantValue: nil,
			wantOK:    false,
		},
		{
			name:      "whitespace only",
			annots:    map[string]string{CORSAllowOrigin: "  ,  ,  "},
			key:       CORSAllowOrigin,
			wantValue: nil,
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewAnnotationSet(tt.annots)
			got, ok := as.GetStringSlice(tt.key)
			if ok != tt.wantOK {
				t.Errorf("GetStringSlice() ok = %v, want %v", ok, tt.wantOK)
			}
			if len(got) != len(tt.wantValue) {
				t.Errorf("GetStringSlice() len = %v, want %v", len(got), len(tt.wantValue))
			}
			for i := range got {
				if got[i] != tt.wantValue[i] {
					t.Errorf("GetStringSlice()[%d] = %v, want %v", i, got[i], tt.wantValue[i])
				}
			}
		})
	}
}

func TestHasTimeout(t *testing.T) {
	tests := []struct {
		name   string
		annots map[string]string
		want   bool
	}{
		{
			name:   "has read timeout",
			annots: map[string]string{ProxyReadTimeout: "30"},
			want:   true,
		},
		{
			name:   "has send timeout",
			annots: map[string]string{ProxySendTimeout: "30"},
			want:   true,
		},
		{
			name:   "no timeout",
			annots: map[string]string{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewAnnotationSet(tt.annots)
			if got := as.HasTimeout(); got != tt.want {
				t.Errorf("HasTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasCORS(t *testing.T) {
	tests := []struct {
		name   string
		annots map[string]string
		want   bool
	}{
		{
			name:   "enabled explicitly",
			annots: map[string]string{CORSEnabled: "true"},
			want:   true,
		},
		{
			name:   "has allow origin",
			annots: map[string]string{CORSAllowOrigin: "*"},
			want:   true,
		},
		{
			name:   "has allow methods",
			annots: map[string]string{CORSAllowMethods: "GET,POST"},
			want:   true,
		},
		{
			name:   "no CORS",
			annots: map[string]string{},
			want:   false,
		},
		{
			name:   "explicitly disabled",
			annots: map[string]string{CORSEnabled: "false"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewAnnotationSet(tt.annots)
			if got := as.HasCORS(); got != tt.want {
				t.Errorf("HasCORS() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasExtAuth(t *testing.T) {
	tests := []struct {
		name   string
		annots map[string]string
		want   bool
	}{
		{
			name:   "has auth-url",
			annots: map[string]string{AuthURL: "http://auth.default.svc.cluster.local/verify"},
			want:   true,
		},
		{
			name:   "no auth-url",
			annots: map[string]string{AuthSignin: "http://login.example.com"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewAnnotationSet(tt.annots)
			if got := as.HasExtAuth(); got != tt.want {
				t.Errorf("HasExtAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasHTTPRouteFilters(t *testing.T) {
	tests := []struct {
		name   string
		annots map[string]string
		want   bool
	}{
		{
			name:   "has rewrite",
			annots: map[string]string{RewriteTarget: "/"},
			want:   true,
		},
		{
			name:   "has app-root",
			annots: map[string]string{AppRoot: "/app"},
			want:   true,
		},
		{
			name:   "has ssl-redirect",
			annots: map[string]string{SSLRedirect: "true"},
			want:   true,
		},
		{
			name:   "ssl-redirect false",
			annots: map[string]string{SSLRedirect: "false"},
			want:   false,
		},
		{
			name:   "no filters",
			annots: map[string]string{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewAnnotationSet(tt.annots)
			if got := as.HasHTTPRouteFilters(); got != tt.want {
				t.Errorf("HasHTTPRouteFilters() = %v, want %v", got, tt.want)
			}
		})
	}
}
