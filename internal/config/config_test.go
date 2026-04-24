package config

import (
	"os"
	"strings"
	"testing"
)

func TestPersonalDataHasContent(t *testing.T) {
	cases := []struct {
		name     string
		pd       PersonalData
		expected bool
	}{
		{"empty", PersonalData{}, false},
		{"full_name only", PersonalData{FullName: "Иванов Иван"}, true},
		{"phone only", PersonalData{Phone: "+375291234567"}, true},
		{"last_name only", PersonalData{LastName: "Иванов"}, true},
		{"first_name only", PersonalData{FirstName: "Иван"}, true},
		{"middle_name only", PersonalData{MiddleName: "Иванович"}, true},
		{"separate fields", PersonalData{LastName: "Иванов", FirstName: "Иван", MiddleName: "Иванович"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.pd.HasContent()
			if got != tc.expected {
				t.Errorf("HasContent() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestInitConfigMessage_NoHTTPTypo(t *testing.T) {
	src, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatalf("read config.go: %v", err)
	}
	if strings.Contains(string(src), "worker-ghb-http") {
		t.Error("config.go still contains 'worker-ghb-http' — update to 'worker-ghb-playwright'")
	}
}
