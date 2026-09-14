# Third-party components of SenHub Agent

The components below are linked into the distributed binary. They are
published by third parties under their own licences, which apply to
those components alone.

This list is generated from the build's dependency graph rather than
from the module file, so it describes what actually ships rather than
what is declared: a module used only by the tests does not appear here.

This page corresponds to Annex 1 of the [SenHub Agent license agreement](agreement-fr.md), and is refreshed with every published version.

## Summary

| Licence | Components |
|---|---|
| Apache-2.0 | 54 |
| MIT | 34 |
| BSD-3-Clause | 28 |
| BSD-2-Clause | 6 |
| MPL-2.0 | 4 |
| ISC | 1 |
| Zlib | 1 |

Total: 128 components.

## Detail

| Component | Version | Licence |
|---|---|---|
| `aead.dev/minisign` | v0.2.0 | MIT |
| `filippo.io/age` | v1.3.1 | BSD-3-Clause |
| `filippo.io/edwards25519` | v1.2.0 | BSD-3-Clause |
| `filippo.io/hpke` | v0.4.0 | BSD-3-Clause |
| `github.com/IBM/sarama` | v1.50.2 | MIT |
| `github.com/alecthomas/participle` | v0.4.1 | MIT |
| `github.com/alexflint/go-arg` | v1.6.1 | BSD-2-Clause |
| `github.com/alexflint/go-scalar` | v1.2.0 | BSD-2-Clause |
| `github.com/avast/retry-go/v4` | v4.7.0 | MIT |
| `github.com/cenkalti/backoff/v5` | v5.0.3 | MIT |
| `github.com/cespare/xxhash/v2` | v2.3.0 | MIT |
| `github.com/coreos/go-systemd/v22` | v22.7.0 | Apache-2.0 |
| `github.com/davecgh/go-spew` | v1.1.2-0.20180830191138-d8f796af33cc | ISC |
| `github.com/eapache/go-resiliency` | v1.7.0 | MIT |
| `github.com/emicklei/go-restful/v3` | v3.13.0 | MIT |
| `github.com/fsnotify/fsnotify` | v1.10.1 | BSD-2-Clause |
| `github.com/fxamacker/cbor/v2` | v2.9.0 | MIT |
| `github.com/go-logr/logr` | v1.4.4 | Apache-2.0 |
| `github.com/go-logr/stdr` | v1.2.2 | Apache-2.0 |
| `github.com/go-openapi/jsonpointer` | v0.21.0 | Apache-2.0 |
| `github.com/go-openapi/jsonreference` | v0.20.2 | Apache-2.0 |
| `github.com/go-openapi/swag` | v0.23.0 | Apache-2.0 |
| `github.com/go-sql-driver/mysql` | v1.10.0 | MPL-2.0 |
| `github.com/goburrow/modbus` | v0.1.0 | BSD-2-Clause |
| `github.com/goburrow/serial` | v0.1.0 | MIT |
| `github.com/godbus/dbus/v5` | v5.1.0 | BSD-2-Clause |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | MIT |
| `github.com/golang-sql/civil` | v0.0.0-20220223132316-b832511892a9 | Apache-2.0 |
| `github.com/golang-sql/sqlexp` | v0.1.0 | BSD-3-Clause |
| `github.com/golang/snappy` | v0.0.4 | BSD-3-Clause |
| `github.com/google/gnostic-models` | v0.7.0 | Apache-2.0 |
| `github.com/google/uuid` | v1.6.0 | BSD-3-Clause |
| `github.com/gorilla/mux` | v1.8.1 | BSD-3-Clause |
| `github.com/gosnmp/gosnmp` | v1.43.2 | BSD-3-Clause |
| `github.com/grpc-ecosystem/grpc-gateway/v2` | v2.29.0 | BSD-3-Clause |
| `github.com/hashicorp/go-uuid` | v1.0.3 | MPL-2.0 |
| `github.com/hashicorp/go-version` | v1.9.0 | MPL-2.0 |
| `github.com/jackc/pgpassfile` | v1.0.0 | MIT |
| `github.com/jackc/pgservicefile` | v0.0.0-20240606120523-5a60cdf6a761 | MIT |
| `github.com/jackc/pgx/v5` | v5.9.2 | MIT |
| `github.com/jackc/puddle/v2` | v2.2.2 | MIT |
| `github.com/jcmturner/aescts/v2` | v2.0.0 | Apache-2.0 |
| `github.com/jcmturner/dnsutils/v2` | v2.0.0 | Apache-2.0 |
| `github.com/jcmturner/gofork` | v1.7.6 | BSD-3-Clause |
| `github.com/jcmturner/gokrb5/v8` | v8.4.4 | Apache-2.0 |
| `github.com/jcmturner/rpc/v2` | v2.0.3 | Apache-2.0 |
| `github.com/josharian/intern` | v1.0.0 | MIT |
| `github.com/json-iterator/go` | v1.1.12 | MIT |
| `github.com/kardianos/service` | v1.2.4 | Zlib |
| `github.com/klauspost/compress` | v1.18.6 | Apache-2.0 |
| `github.com/mailru/easyjson` | v0.7.7 | MIT |
| `github.com/mattn/go-colorable` | v0.1.14 | MIT |
| `github.com/mattn/go-isatty` | v0.0.22 | MIT |
| `github.com/microsoft/go-mssqldb` | v1.10.0 | BSD-3-Clause |
| `github.com/minio/selfupdate` | v0.6.0 | Apache-2.0 |
| `github.com/modern-go/concurrent` | v0.0.0-20180306012644-bacd9c7ef1dd | Apache-2.0 |
| `github.com/modern-go/reflect2` | v1.0.3-0.20250322232337-35a7c28c31ee | Apache-2.0 |
| `github.com/montanaflynn/stats` | v0.7.1 | MIT |
| `github.com/munnerz/goautoneg` | v0.0.0-20191010083416-a7dc8b61c822 | BSD-3-Clause |
| `github.com/nxadm/tail` | v1.4.11 | MIT |
| `github.com/pierrec/lz4/v4` | v4.1.27 | BSD-3-Clause |
| `github.com/prometheus-community/pro-bing` | v0.9.0 | MIT |
| `github.com/prometheus/client_model` | v0.6.2 | Apache-2.0 |
| `github.com/prometheus/common` | v0.67.5 | Apache-2.0 |
| `github.com/rcrowley/go-metrics` | v0.0.0-20250401214520-65e299d6c5c9 | BSD-2-Clause |
| `github.com/rs/zerolog` | v1.35.1 | MIT |
| `github.com/shirou/gopsutil/v3` | v3.24.5 | BSD-3-Clause |
| `github.com/shoenig/go-m1cpu` | v0.1.6 | MPL-2.0 |
| `github.com/shopspring/decimal` | v1.4.0 | MIT |
| `github.com/sijms/go-ora/v2` | v2.9.0 | MIT |
| `github.com/sleepinggenius2/gosmi` | v0.4.4 | MIT |
| `github.com/spf13/pflag` | v1.0.9 | BSD-3-Clause |
| `github.com/tklauser/go-sysconf` | v0.3.12 | BSD-3-Clause |
| `github.com/toise-dev/toise/pkg/emit` | v0.9.0 | Apache-2.0 |
| `github.com/x448/float16` | v0.8.4 | MIT |
| `github.com/xdg-go/pbkdf2` | v1.0.0 | Apache-2.0 |
| `github.com/xdg-go/scram` | v1.1.2 | Apache-2.0 |
| `github.com/xdg-go/stringprep` | v1.0.4 | Apache-2.0 |
| `github.com/ybbus/httpretry` | v1.0.2 | MIT |
| `github.com/youmark/pkcs8` | v0.0.0-20240726163527-a2c0da244d78 | MIT |
| `go.mongodb.org/mongo-driver` | v1.17.9 | Apache-2.0 |
| `go.opentelemetry.io/auto/sdk` | v1.2.1 | Apache-2.0 |
| `go.opentelemetry.io/otel` | v1.45.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc` | v0.21.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp` | v0.21.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc` | v1.45.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp` | v1.45.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace` | v1.45.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | v1.45.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` | v1.45.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/log` | v0.21.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/metric` | v1.45.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/sdk` | v1.45.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/sdk/log` | v0.21.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/sdk/metric` | v1.45.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/trace` | v1.45.0 | Apache-2.0 |
| `go.opentelemetry.io/proto/otlp` | v1.11.0 | Apache-2.0 |
| `go.yaml.in/yaml/v2` | v2.4.3 | Apache-2.0 |
| `go.yaml.in/yaml/v3` | v3.0.4 | MIT |
| `golang.org/x/crypto` | v0.54.0 | BSD-3-Clause |
| `golang.org/x/net` | v0.57.0 | BSD-3-Clause |
| `golang.org/x/oauth2` | v0.36.0 | BSD-3-Clause |
| `golang.org/x/sync` | v0.22.0 | BSD-3-Clause |
| `golang.org/x/sys` | v0.47.0 | BSD-3-Clause |
| `golang.org/x/term` | v0.45.0 | BSD-3-Clause |
| `golang.org/x/text` | v0.40.0 | BSD-3-Clause |
| `golang.org/x/time` | v0.14.0 | BSD-3-Clause |
| `google.golang.org/genproto/googleapis/api` | v0.0.0-20260803160001-6ac0973c030d | Apache-2.0 |
| `google.golang.org/genproto/googleapis/rpc` | v0.0.0-20260803160001-6ac0973c030d | Apache-2.0 |
| `google.golang.org/grpc` | v1.83.0 | Apache-2.0 |
| `google.golang.org/protobuf` | v1.36.12-0.20260120151049-f2248ac996af | BSD-3-Clause |
| `gopkg.in/evanphx/json-patch.v4` | v4.13.0 | BSD-3-Clause |
| `gopkg.in/inf.v0` | v0.9.1 | BSD-3-Clause |
| `gopkg.in/mcuadros/go-syslog.v2` | v2.3.0 | MIT |
| `gopkg.in/natefinch/lumberjack.v2` | v2.2.1 | MIT |
| `gopkg.in/tomb.v1` | v1.0.0-20141024135613-dd632973f1e7 | BSD-3-Clause |
| `gopkg.in/yaml.v2` | v2.4.0 | Apache-2.0 |
| `gopkg.in/yaml.v3` | v3.0.1 | MIT |
| `k8s.io/api` | v0.36.2 | Apache-2.0 |
| `k8s.io/apimachinery` | v0.36.2 | Apache-2.0 |
| `k8s.io/client-go` | v0.36.2 | Apache-2.0 |
| `k8s.io/klog/v2` | v2.140.0 | Apache-2.0 |
| `k8s.io/kube-openapi` | v0.0.0-20260317180543-43fb72c5454a | Apache-2.0 |
| `k8s.io/utils` | v0.0.0-20260210185600-b8788abfbbc2 | Apache-2.0 |
| `sigs.k8s.io/json` | v0.0.0-20250730193827-2d320260d730 | Apache-2.0 |
| `sigs.k8s.io/randfill` | v1.0.0 | Apache-2.0 |
| `sigs.k8s.io/structured-merge-diff/v6` | v6.3.2 | Apache-2.0 |
| `sigs.k8s.io/yaml` | v1.6.0 | Apache-2.0 |
