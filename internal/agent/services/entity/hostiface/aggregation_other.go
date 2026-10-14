//go:build !linux

package hostiface

func platformAggregation() (map[string]*aggregationInfo, error) {
	return nil, nil
}
