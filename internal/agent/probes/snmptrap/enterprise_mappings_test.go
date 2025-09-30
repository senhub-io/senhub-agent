package snmptrap

import (
	"testing"
)

func TestGetEnterpriseFromOID(t *testing.T) {
	tests := []struct {
		name         string
		oid          string
		expectedName string
		expectedFull string
		expectedCat  string
		shouldBeNil  bool
	}{
		{
			name:         "Cisco OID",
			oid:          "1.3.6.1.4.1.9.9.41.2.0.1",
			expectedName: "cisco",
			expectedFull: "Cisco Systems",
			expectedCat:  "network",
			shouldBeNil:  false,
		},
		{
			name:         "Palo Alto OID",
			oid:          "1.3.6.1.4.1.25461.2.1.3.2",
			expectedName: "paloalto",
			expectedFull: "Palo Alto Networks",
			expectedCat:  "security",
			shouldBeNil:  false,
		},
		{
			name:         "Fortinet OID",
			oid:          "1.3.6.1.4.1.12356.101.6.0.1",
			expectedName: "fortinet",
			expectedFull: "Fortinet",
			expectedCat:  "security",
			shouldBeNil:  false,
		},
		{
			name:         "Huawei OID",
			oid:          "1.3.6.1.4.1.2011.5.25.129.2.1.1",
			expectedName: "huawei",
			expectedFull: "Huawei",
			expectedCat:  "network",
			shouldBeNil:  false,
		},
		{
			name:         "Unknown enterprise",
			oid:          "1.3.6.1.4.1.99999.1.2.3",
			expectedName: "enterprise_99999",
			expectedFull: "Unknown Enterprise (99999)",
			expectedCat:  "unknown",
			shouldBeNil:  false,
		},
		{
			name:        "Invalid OID format",
			oid:         "invalid.oid.format",
			shouldBeNil: true,
		},
		{
			name:        "Empty OID",
			oid:         "",
			shouldBeNil: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enterprise := GetEnterpriseFromOID(tt.oid)
			
			if tt.shouldBeNil {
				if enterprise != nil {
					t.Errorf("Expected nil enterprise info, got %+v", enterprise)
				}
				return
			}
			
			if enterprise == nil {
				t.Error("Expected enterprise info, got nil")
				return
			}
			
			if enterprise.Name != tt.expectedName {
				t.Errorf("Expected name '%s', got '%s'", tt.expectedName, enterprise.Name)
			}
			
			if enterprise.FullName != tt.expectedFull {
				t.Errorf("Expected full name '%s', got '%s'", tt.expectedFull, enterprise.FullName)
			}
			
			if enterprise.Category != tt.expectedCat {
				t.Errorf("Expected category '%s', got '%s'", tt.expectedCat, enterprise.Category)
			}
		})
	}
}

func TestGetCategoryPriority(t *testing.T) {
	tests := []struct {
		category         string
		expectedPriority int
	}{
		{"security", 1},
		{"network", 2},
		{"loadbalancer", 3},
		{"server", 4},
		{"datacenter", 5},
		{"wireless", 6},
		{"wan", 7},
		{"monitoring", 8},
		{"soho", 9},
		{"agent", 10},
		{"unknown", 99},
		{"nonexistent", 99},
	}
	
	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			priority := GetCategoryPriority(tt.category)
			if priority != tt.expectedPriority {
				t.Errorf("Expected priority %d for category '%s', got %d",
					tt.expectedPriority, tt.category, priority)
			}
		})
	}
}

func TestIsAllowedEnterprise(t *testing.T) {
	tests := []struct {
		name           string
		enterpriseOID  string
		allowedList    []string
		expectedResult bool
	}{
		{
			name:           "empty allowed list allows all",
			enterpriseOID:  "1.3.6.1.4.1.9",
			allowedList:    []string{},
			expectedResult: true,
		},
		{
			name:           "exact match in allowed list",
			enterpriseOID:  "1.3.6.1.4.1.9",
			allowedList:    []string{"1.3.6.1.4.1.9", "1.3.6.1.4.1.25461"},
			expectedResult: true,
		},
		{
			name:           "prefix match in allowed list",
			enterpriseOID:  "1.3.6.1.4.1.9.9.41.2.0.1",
			allowedList:    []string{"1.3.6.1.4.1.9"},
			expectedResult: true,
		},
		{
			name:           "not in allowed list",
			enterpriseOID:  "1.3.6.1.4.1.12356",
			allowedList:    []string{"1.3.6.1.4.1.9", "1.3.6.1.4.1.25461"},
			expectedResult: false,
		},
		{
			name:           "partial match but not prefix",
			enterpriseOID:  "1.3.6.1.4.1.999",
			allowedList:    []string{"1.3.6.1.4.1.99"},
			expectedResult: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAllowedEnterprise(tt.enterpriseOID, tt.allowedList)
			if result != tt.expectedResult {
				t.Errorf("Expected %v for OID '%s' with allowed list %v, got %v",
					tt.expectedResult, tt.enterpriseOID, tt.allowedList, result)
			}
		})
	}
}

func TestKnownEnterprises(t *testing.T) {
	// Test that all major vendors are included
	majorVendors := []struct {
		oid          string
		expectedName string
		expectedCat  string
	}{
		{"9", "cisco", "network"},
		{"25461", "paloalto", "security"},
		{"12356", "fortinet", "security"},
		{"2011", "huawei", "network"},
		{"2636", "juniper", "network"},
		{"3375", "f5", "loadbalancer"},
		{"674", "dell", "server"},
		{"232", "hpe", "server"},
	}
	
	for _, vendor := range majorVendors {
		t.Run(vendor.expectedName, func(t *testing.T) {
			info, exists := KnownEnterprises[vendor.oid]
			if !exists {
				t.Errorf("Expected vendor '%s' (OID %s) to be in KnownEnterprises", 
					vendor.expectedName, vendor.oid)
				return
			}
			
			if info.Name != vendor.expectedName {
				t.Errorf("Expected name '%s', got '%s'", vendor.expectedName, info.Name)
			}
			
			if info.Category != vendor.expectedCat {
				t.Errorf("Expected category '%s', got '%s'", vendor.expectedCat, info.Category)
			}
			
			if info.FullName == "" {
				t.Error("FullName should not be empty")
			}
		})
	}
	
	// Test that we have a reasonable number of vendors
	if len(KnownEnterprises) < 50 {
		t.Errorf("Expected at least 50 known enterprises, got %d", len(KnownEnterprises))
	}
	
	// Test that all entries have required fields
	for oid, info := range KnownEnterprises {
		if info.Name == "" {
			t.Errorf("Enterprise OID %s has empty Name", oid)
		}
		if info.FullName == "" {
			t.Errorf("Enterprise OID %s has empty FullName", oid)
		}
		if info.Category == "" {
			t.Errorf("Enterprise OID %s has empty Category", oid)
		}
	}
}