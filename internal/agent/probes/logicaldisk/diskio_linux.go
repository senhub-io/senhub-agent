//go:build linux

package logicaldisk

import (
	"fmt"
	"os"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

const procDiskstats = "/proc/diskstats"

func (c *unixLogicalDiskCollector) collectDiskIO(ts time.Time, base []tags.Tag) ([]data_store.DataPoint, error) {
	f, err := os.Open(procDiskstats)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", procDiskstats, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			c.logger.Debug().Err(cerr).Msg("closing diskstats")
		}
	}()
	devs, err := parseDiskstats(f)
	if err != nil {
		return nil, err
	}
	return diskIOPoints(devs, base, ts), nil
}
