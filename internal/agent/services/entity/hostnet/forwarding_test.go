package hostnet

import "testing"

// TestWithForwarding checks each family lands on the host entity on its
// own, and that an unread switch stays absent rather than false.
func TestWithForwarding(t *testing.T) {
	host := func(v4, v6 string) map[string]any {
		obs := withForwarding(buildObservation("h1", nil), "h1", v4, v6)
		e, _ := entityOfType(obs.Entities, entityTypeHost)
		return e.Attributes
	}
	a := host("1", "0")
	if a[attrIPForwardingV4] != true || a[attrIPForwardingV6] != false {
		t.Errorf("v4 on, v6 off = %v", a)
	}
	a = host("0", "")
	if a[attrIPForwardingV4] != false {
		t.Errorf("v4 off = %v", a)
	}
	if _, ok := a[attrIPForwardingV6]; ok {
		t.Errorf("unread v6 must stay absent: %v", a)
	}
	a = host("", "")
	if _, ok := a[attrIPForwardingV4]; ok {
		t.Errorf("unread v4 must stay absent: %v", a)
	}
}
