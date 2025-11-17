package license

// SenHubPublicKey is the RSA public key used to verify license tokens
// This key is embedded in the binary and corresponds to the private key
// used by SenHub platform to sign licenses
//
// SECURITY: The corresponding private key (license-private-key.pem) must be kept
// SECRET and stored securely in Sensor Factory. Only Sensor Factory can sign licenses.
const SenHubPublicKey = `-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA3QU/F/S/VGrT/4VO881k
8YtlcDniOPOS7Lk0lfzjm4c2Zbj0VmX3AMpAgdm23f6Enw8CkAUFayVeQKsteyqj
i90USB6l1XSRT8WI3fE9BDQJNqU6omMT5GmDqyYuG3qIuO4CsGt20xw640hOX/uZ
OfA6+5fcQPPQ4KPH1AbKzlNX+XPy0tcbaZmzD62MSpk/z5t3YWFrkP9QkLBNeZtP
MmHA1mXjLwnlVTSKw7Ka74+YlRv2ki2BFyy7ZuUU3/1k1DYCIcYHn7/KaQ5Wq0in
UCFKMmmREmuKEkJFXBvj24Q+STMKOk8Odo7jlQEmqYuLx7sR/zEVIr9thymtck2p
/3HDklX3iz44MpDPiBOjJux92Hsok4WRH0Jk1gyYIAE8T2O6eQLp95s39I6JANI+
DtTSDTbr+RLDYNO3T0CsqkjsCmQsNe8z6P208RqpPHpFHpx5gkXCsfo/soxXQj4T
7phlu9HY/ghNx61azGWBvyNeJlfpec6qcs4bXujyOcZ3m5Waf8zWND4YJz+lRUK1
He6ytSsL3cgE3ou9azmNmKWUsAI7SrVZZEx+08m13tOKfHiy97MhVdCJnKA2K92c
lxJ4nelVv8iHaYePoYKfjrlQdjbg13Ac65d2a6Ab4scOOZXOFoQiz88Tg58DktQU
oJANBrDc6hXfPNnf9gtgWwkCAwEAAQ==
-----END PUBLIC KEY-----`

// GetDefaultValidator returns a validator configured with the embedded SenHub public key
func GetDefaultValidator(gracePeriodDays int) (*JWTValidator, error) {
	return NewJWTValidator(SenHubPublicKey, gracePeriodDays)
}
