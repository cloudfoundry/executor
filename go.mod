module code.cloudfoundry.org/executor

go 1.25.5

replace (
	code.cloudfoundry.org/rep => ../rep
	code.cloudfoundry.org/volman => ../volman
	google.golang.org/genproto => google.golang.org/genproto/googleapis/rpc v0.0.0-20260128011058-8636f8732409
)

require (
	code.cloudfoundry.org/archiver v0.60.0
	code.cloudfoundry.org/bbs v0.0.0-20260107153229-7b22834eb1d7
	code.cloudfoundry.org/bytefmt v0.62.0
	code.cloudfoundry.org/cacheddownloader v0.0.0-20250312193827-23c030d5e4f3
	code.cloudfoundry.org/clock v1.59.0
	code.cloudfoundry.org/diego-logging-client v0.89.0
	code.cloudfoundry.org/durationjson v0.62.0
	code.cloudfoundry.org/eventhub v0.62.0
	code.cloudfoundry.org/garden v0.0.0-20260121023424-879cfc366958
	code.cloudfoundry.org/go-loggregator/v9 v9.2.1
	code.cloudfoundry.org/lager/v3 v3.59.0
	code.cloudfoundry.org/routing-info v0.0.0-20250117183711-d8d8d2ad4608
	code.cloudfoundry.org/tlsconfig v0.44.0
	code.cloudfoundry.org/volman v0.0.0-00010101000000-000000000000
	code.cloudfoundry.org/workpool v0.0.0-20250911194158-1489753f182e
	github.com/envoyproxy/go-control-plane/envoy v1.36.0
	github.com/fsnotify/fsnotify v1.9.0
	github.com/ghodss/yaml v1.0.0
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.5.4
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/hashicorp/errwrap v1.1.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/nu7hatch/gouuid v0.0.0-20131221200532-179d4d0c4d8d
	github.com/onsi/ginkgo/v2 v2.28.1
	github.com/onsi/gomega v1.39.1
	github.com/tedsuo/ifrit v0.0.0-20230516164442-7862c310ad26
	golang.org/x/time v0.14.0
	google.golang.org/protobuf v1.36.11
)

require (
	cel.dev/expr v0.24.0 // indirect
	code.cloudfoundry.org/cfhttp/v2 v2.67.0 // indirect
	code.cloudfoundry.org/dockerdriver v0.72.0 // indirect
	code.cloudfoundry.org/go-diodes v0.0.0-20260119094648-9c5f37160881 // indirect
	code.cloudfoundry.org/goshims v0.89.0 // indirect
	code.cloudfoundry.org/locket v0.0.0-20251117222557-be612341b29d // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/bmizerany/pat v0.0.0-20210406213842-e4b6760bdd6f // indirect
	github.com/cncf/xds/go v0.0.0-20251022180443-0feb69152e9f // indirect
	github.com/cyphar/filepath-securejoin v0.6.1 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.2.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/go-test/deep v1.1.1 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20260115054156-294ebfa9ad83 // indirect
	github.com/jackc/pgx/v5 v5.8.0 // indirect
	github.com/openzipkin/zipkin-go v0.4.3 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/tedsuo/rata v1.0.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/mod v0.32.0 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	golang.org/x/tools v0.41.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251029180050-ab9386a59fda // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260126211449-d11affda4bed // indirect
	google.golang.org/grpc v1.78.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
