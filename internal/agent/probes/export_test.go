package probes

// CollectOnce runs one collection cycle the way the scheduler does.
func (p *ProbePoller) CollectOnce() error { return p.collect() }
