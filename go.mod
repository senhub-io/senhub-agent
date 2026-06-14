module senhub-agent.go

go 1.26.4

require (
	github.com/Azure/go-ntlmssp v0.1.1
	github.com/alexflint/go-arg v1.6.1
	github.com/avast/retry-go/v4 v4.7.0
	github.com/citrix/adc-nitro-go v0.0.0-20250915211247-deb279797e53
	github.com/fsnotify/fsnotify v1.10.1
	github.com/go-sql-driver/mysql v1.10.0
	github.com/go-test/deep v1.1.1
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/gorilla/mux v1.8.1
	github.com/gosnmp/gosnmp v1.43.2
	github.com/hashicorp/go-version v1.9.0
	github.com/jackc/pgx/v5 v5.9.2
	github.com/kardianos/service v1.2.4
	github.com/minio/selfupdate v0.6.0
	github.com/nxadm/tail v1.4.11
	github.com/prometheus/client_model v0.6.2
	github.com/prometheus/common v0.67.5
	github.com/rs/zerolog v1.35.1
	github.com/shirou/gopsutil/v3 v3.24.5
	github.com/sleepinggenius2/gosmi v0.4.4
	github.com/stretchr/testify v1.11.1
	github.com/ybbus/httpretry v1.0.2
	github.com/yusufpapurcu/wmi v1.2.4
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.19.0
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.19.0
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.43.0
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.43.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.43.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.43.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.43.0
	go.opentelemetry.io/otel/log v0.19.0
	go.opentelemetry.io/otel/sdk v1.43.0
	go.opentelemetry.io/otel/sdk/log v0.19.0
	go.opentelemetry.io/otel/sdk/metric v1.43.0
	go.opentelemetry.io/otel/trace v1.43.0
	golang.org/x/sys v0.45.0
	golang.org/x/text v0.37.0
	google.golang.org/grpc v1.81.0
	gopkg.in/mcuadros/go-syslog.v2 v2.3.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/IBM/sarama v1.50.2 // indirect
	github.com/eapache/go-resiliency v1.7.0 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/jcmturner/aescts/v2 v2.0.0 // indirect
	github.com/jcmturner/dnsutils/v2 v2.0.0 // indirect
	github.com/jcmturner/gofork v1.7.6 // indirect
	github.com/jcmturner/gokrb5/v8 v8.4.4 // indirect
	github.com/jcmturner/rpc/v2 v2.0.3 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/pierrec/lz4/v4 v4.1.27 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20250401214520-65e299d6c5c9 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
)

require (
	aead.dev/minisign v0.2.0
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/alecthomas/participle v0.4.1 // indirect
	github.com/alexflint/go-scalar v1.2.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/fatih/color v1.7.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/hashicorp/go-hclog v0.16.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/prometheus-community/pro-bing v0.9.0
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	golang.org/x/crypto v0.52.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
)

// TEMPORARY FORK: Using senhub-io fork with singleton stats fix
// This can be removed once upstream merges PR #36
// Upstream PR: https://github.com/citrix/adc-nitro-go/pull/36
// Our fix (FindAllStats + FindStat): https://github.com/senhub-io/adc-nitro-go/commit/d944ae6434d1c8f6eeca0fca9cefd98a4b546429
replace github.com/citrix/adc-nitro-go => github.com/senhub-io/adc-nitro-go v0.0.0-20251211102010-d944ae6434d1
