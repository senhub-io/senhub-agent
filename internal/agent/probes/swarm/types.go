package swarm

// Docker Engine API response shapes, narrowed to the fields the probe reads.
//
// Decoded into hand-written structs rather than the Docker SDK: the docker
// probe already proved the Engine API is comfortable over stdlib net/http, and
// pulling the SDK in would add a large dependency tree to a binary whose whole
// pitch is that it has none.
//
// Every struct is deliberately partial. The Engine API returns far more than
// this, and decoding only what is used keeps an upstream field addition from
// breaking the parse.

// swarmInfo is GET /swarm. JoinTokens are present in the response and are
// credentials — they are never decoded, so they cannot be logged by accident.
type swarmInfo struct {
	ID        string `json:"ID"`
	CreatedAt string `json:"CreatedAt"`
	Spec      struct {
		Name string `json:"Name"`
	} `json:"Spec"`
}

// node is one entry of GET /nodes.
type node struct {
	ID          string `json:"ID"`
	Description struct {
		Hostname string `json:"Hostname"`
		Platform struct {
			Architecture string `json:"Architecture"`
			OS           string `json:"OS"`
		} `json:"Platform"`
		Engine struct {
			EngineVersion string `json:"EngineVersion"`
		} `json:"Engine"`
		Resources struct {
			NanoCPUs    int64 `json:"NanoCPUs"`
			MemoryBytes int64 `json:"MemoryBytes"`
		} `json:"Resources"`
	} `json:"Description"`
	Spec struct {
		Role         string `json:"Role"`         // manager | worker
		Availability string `json:"Availability"` // active | pause | drain
	} `json:"Spec"`
	Status struct {
		State   string `json:"State"` // ready | down | unknown | disconnected
		Message string `json:"Message"`
		Addr    string `json:"Addr"`
	} `json:"Status"`
	// ManagerStatus is absent on workers.
	ManagerStatus *struct {
		Leader       bool   `json:"Leader"`
		Reachability string `json:"Reachability"` // reachable | unreachable | unknown
		Addr         string `json:"Addr"`
	} `json:"ManagerStatus"`
}

// service is one entry of GET /services.
type service struct {
	ID   string `json:"ID"`
	Spec struct {
		Name   string            `json:"Name"`
		Labels map[string]string `json:"Labels"`
		Mode   struct {
			Replicated *struct {
				Replicas *int64 `json:"Replicas"`
			} `json:"Replicated"`
			Global *struct{} `json:"Global"`
		} `json:"Mode"`
		TaskTemplate struct {
			ContainerSpec struct {
				Image string `json:"Image"`
			} `json:"ContainerSpec"`
			Networks []networkAttachmentConfig `json:"Networks"`
		} `json:"TaskTemplate"`
		Networks []networkAttachmentConfig `json:"Networks"` // legacy placement
	} `json:"Spec"`
	Endpoint struct {
		Spec struct {
			Mode  string       `json:"Mode"` // vip | dnsrr
			Ports []portConfig `json:"Ports"`
		} `json:"Spec"`
		Ports      []portConfig `json:"Ports"`
		VirtualIPs []struct {
			NetworkID string `json:"NetworkID"`
			Addr      string `json:"Addr"`
		} `json:"VirtualIPs"`
	} `json:"Endpoint"`
	UpdateStatus *struct {
		State   string `json:"State"` // updating | paused | completed | rollback_*
		Message string `json:"Message"`
	} `json:"UpdateStatus"`
}

type networkAttachmentConfig struct {
	Target  string   `json:"Target"` // network id or name
	Aliases []string `json:"Aliases"`
}

type portConfig struct {
	Protocol      string `json:"Protocol"` // tcp | udp | sctp
	TargetPort    int64  `json:"TargetPort"`
	PublishedPort int64  `json:"PublishedPort"`
	PublishMode   string `json:"PublishMode"` // ingress | host
}

// task is one entry of GET /tasks.
type task struct {
	ID           string `json:"ID"`
	ServiceID    string `json:"ServiceID"`
	NodeID       string `json:"NodeID"`
	Slot         int64  `json:"Slot"`
	DesiredState string `json:"DesiredState"`
	Status       struct {
		State           string `json:"State"`
		Message         string `json:"Message"`
		Err             string `json:"Err"`
		ContainerStatus *struct {
			ContainerID string `json:"ContainerID"`
			ExitCode    int64  `json:"ExitCode"`
		} `json:"ContainerStatus"`
	} `json:"Status"`
	NetworksAttachments []struct {
		Network struct {
			ID   string `json:"ID"`
			Spec struct {
				Name string `json:"Name"`
			} `json:"Spec"`
		} `json:"Network"`
		Addresses []string `json:"Addresses"`
	} `json:"NetworksAttachments"`
}

// network is one entry of GET /networks.
type network struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Driver string `json:"Driver"` // overlay | bridge | macvlan | …
	Scope  string `json:"Scope"`  // swarm | local
	// Ingress marks the routing-mesh network that carries published ports.
	Ingress    bool `json:"Ingress"`
	Attachable bool `json:"Attachable"`
	Internal   bool `json:"Internal"`
	IPAM       struct {
		Config []struct {
			Subnet  string `json:"Subnet"`
			Gateway string `json:"Gateway"`
		} `json:"Config"`
	} `json:"IPAM"`
	Labels map[string]string `json:"Labels"`
}
