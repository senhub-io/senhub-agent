// senhub-agent/internal/agent/services/data_store/buffer.go
package data_store

import "sync"

type Buffer interface {
	// Append appends data to the buffer
	Append(newData []DataPoint) error
	// Flush the buffer data and return the data
	Sync() []DataPoint
	// Revert the sync operation
	AbortSync(failedData []DataPoint) error
}

type buffer struct {
	data  *[]DataPoint
	mutex sync.Mutex // Ensures thread-safe execution of doRefreshConfig
}

func NewBuffer() Buffer {
	return &buffer{
		data: &[]DataPoint{},
	}
}

func (b *buffer) Append(newData []DataPoint) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	*b.data = append(*b.data, newData...)
	return nil
}

func (b *buffer) Sync() []DataPoint {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	data := *b.data
	*b.data = []DataPoint{}
	return data
}

func (b *buffer) AbortSync(failedData []DataPoint) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	*b.data = append(*b.data, failedData...)
	return nil
}
