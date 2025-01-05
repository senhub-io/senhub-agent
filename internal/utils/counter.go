package utils

type Counter struct {
	value uint32
}

func NewCounter(initialValue uint32) Counter {
	return Counter{
		value: initialValue,
	}
}

func (c *Counter) AddValue(value uint32) {
	c.value += value
}

func (c *Counter) Value() uint32 {
	return c.value
}

func (c *Counter) Reset() {
	c.value = 0
}
