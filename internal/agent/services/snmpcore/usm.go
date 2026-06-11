package snmpcore

import "github.com/gosnmp/gosnmp"

// AuthProtocol maps a config auth-protocol name to the gosnmp constant.
// Unknown or empty names mean no authentication.
func AuthProtocol(name string) gosnmp.SnmpV3AuthProtocol {
	switch name {
	case "MD5":
		return gosnmp.MD5
	case "SHA":
		return gosnmp.SHA
	case "SHA224":
		return gosnmp.SHA224
	case "SHA256":
		return gosnmp.SHA256
	case "SHA384":
		return gosnmp.SHA384
	case "SHA512":
		return gosnmp.SHA512
	default:
		return gosnmp.NoAuth
	}
}

// PrivProtocol maps a config privacy-protocol name to the gosnmp
// constant. Unknown or empty names mean no privacy.
func PrivProtocol(name string) gosnmp.SnmpV3PrivProtocol {
	switch name {
	case "DES":
		return gosnmp.DES
	case "AES":
		return gosnmp.AES
	case "AES192":
		return gosnmp.AES192
	case "AES256":
		return gosnmp.AES256
	default:
		return gosnmp.NoPriv
	}
}

// MsgFlags derives the USM security level from the configured
// protocols: authPriv when both are set, authNoPriv with auth only,
// noAuthNoPriv otherwise.
func MsgFlags(auth gosnmp.SnmpV3AuthProtocol, priv gosnmp.SnmpV3PrivProtocol) gosnmp.SnmpV3MsgFlags {
	switch {
	case auth != gosnmp.NoAuth && priv != gosnmp.NoPriv:
		return gosnmp.AuthPriv
	case auth != gosnmp.NoAuth:
		return gosnmp.AuthNoPriv
	default:
		return gosnmp.NoAuthNoPriv
	}
}
