package data_store

type Buffer interface {
	// Append appends data to the buffer
	Append(newData []DataPoint) error
	// Flush the buffer data and return the data
	Sync() []DataPoint
	// Revert the sync operation
	AbortSync(failedData []DataPoint) error
}

type buffer struct {
	data *[]DataPoint
}

func NewBuffer() Buffer {
	return &buffer{
		data: &[]DataPoint{},
	}
}

func (b *buffer) Append(newData []DataPoint) error {
	*b.data = append(*b.data, newData...)
	return nil
}

func (b *buffer) Sync() []DataPoint {
	data := *b.data
	*b.data = []DataPoint{}
	return data
}

func (b *buffer) AbortSync(failedData []DataPoint) error {
	*b.data = append(*b.data, failedData...)
	return nil
}
