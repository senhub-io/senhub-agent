package kubeletstats

// summary is the subset of the kubelet Summary API this probe consumes.
type summary struct {
	Node nodeStats  `json:"node"`
	Pods []podStats `json:"pods"`
}

type nodeStats struct {
	NodeName string        `json:"nodeName"`
	CPU      *cpuStats     `json:"cpu"`
	Memory   *memoryStats  `json:"memory"`
	Network  *networkStats `json:"network"`
	Fs       *fsStats      `json:"fs"`
}

type podStats struct {
	PodRef     podRef           `json:"podRef"`
	CPU        *cpuStats        `json:"cpu"`
	Memory     *memoryStats     `json:"memory"`
	Network    *networkStats    `json:"network"`
	Containers []containerStats `json:"containers"`
}

type podRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
}

type containerStats struct {
	Name   string       `json:"name"`
	CPU    *cpuStats    `json:"cpu"`
	Memory *memoryStats `json:"memory"`
}

type cpuStats struct {
	Time                 string  `json:"time"`
	UsageNanoCores       *uint64 `json:"usageNanoCores"`
	UsageCoreNanoSeconds *uint64 `json:"usageCoreNanoSeconds"`
}

type memoryStats struct {
	Time            string  `json:"time"`
	AvailableBytes  *uint64 `json:"availableBytes"`
	UsageBytes      *uint64 `json:"usageBytes"`
	WorkingSetBytes *uint64 `json:"workingSetBytes"`
	RSSBytes        *uint64 `json:"rssBytes"`
	PageFaults      *uint64 `json:"pageFaults"`
	MajorPageFaults *uint64 `json:"majorPageFaults"`
}

type networkStats struct {
	Time       string                  `json:"time"`
	Name       string                  `json:"name"`
	RxBytes    *uint64                 `json:"rxBytes"`
	RxErrors   *uint64                 `json:"rxErrors"`
	TxBytes    *uint64                 `json:"txBytes"`
	TxErrors   *uint64                 `json:"txErrors"`
	Interfaces []networkInterfaceStats `json:"interfaces"`
}

type networkInterfaceStats struct {
	Name     string  `json:"name"`
	RxBytes  *uint64 `json:"rxBytes"`
	RxErrors *uint64 `json:"rxErrors"`
	TxBytes  *uint64 `json:"txBytes"`
	TxErrors *uint64 `json:"txErrors"`
}

type fsStats struct {
	Time           string  `json:"time"`
	AvailableBytes *uint64 `json:"availableBytes"`
	CapacityBytes  *uint64 `json:"capacityBytes"`
	UsedBytes      *uint64 `json:"usedBytes"`
	InodesFree     *uint64 `json:"inodesFree"`
	Inodes         *uint64 `json:"inodes"`
	InodesUsed     *uint64 `json:"inodesUsed"`
}
