module code.cloudfoundry.org/executor

go 1.22.3

replace code.cloudfoundry.org/bbs => github.com/dimitardimitrov13/bbs v0.0.0-20240627093200-178c67bee13f

require (
	code.cloudfoundry.org/archiver v0.0.0-20240625174243-6d58e629a167
	code.cloudfoundry.org/bbs v0.0.0-00010101000000-000000000000
	code.cloudfoundry.org/bytefmt v0.0.0-20240625174231-fca5dc407bce
	code.cloudfoundry.org/cacheddownloader v0.0.0-20240408163934-09b8631e33d0
	code.cloudfoundry.org/clock v1.2.0
	code.cloudfoundry.org/diego-logging-client v0.0.0-20240703175007-4cd6b2daa77c
	code.cloudfoundry.org/durationjson v0.0.0-20240625174233-9ff5003698bf
	code.cloudfoundry.org/eventhub v0.0.0-20240625174234-481b921ce364
	code.cloudfoundry.org/garden v0.0.0-20240625195848-36e99aad95da
	code.cloudfoundry.org/go-loggregator/v9 v9.2.1
	code.cloudfoundry.org/lager/v3 v3.0.3
	code.cloudfoundry.org/routing-info v0.0.0-20240611155555-dd78756e41b6
	code.cloudfoundry.org/tlsconfig v0.0.0-20240702174858-4c0df2f29c62
	code.cloudfoundry.org/volman v0.0.0-20240521125855-6a9a624f6807
	code.cloudfoundry.org/workpool v0.0.0-20240408164905-b6c2fa5a80e4
	github.com/envoyproxy/go-control-plane v0.12.0
	github.com/fsnotify/fsnotify v1.7.0
	github.com/ghodss/yaml v1.0.0
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.5.4
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/hashicorp/errwrap v1.1.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/nu7hatch/gouuid v0.0.0-20131221200532-179d4d0c4d8d
	github.com/onsi/ginkgo/v2 v2.19.0
	github.com/onsi/gomega v1.33.1
	github.com/tedsuo/ifrit v0.0.0-20230516164442-7862c310ad26
	golang.org/x/time v0.5.0
	google.golang.org/protobuf v1.34.2
)

require (
	cel.dev/expr v0.15.0 // indirect
	code.cloudfoundry.org/cfhttp/v2 v2.1.0 // indirect
	code.cloudfoundry.org/dockerdriver v0.0.0-20240620154825-441e44b5dbb3 // indirect
	code.cloudfoundry.org/go-diodes v0.0.0-20240604201846-c756bfed2ed3 // indirect
	code.cloudfoundry.org/goshims v0.37.0 // indirect
	code.cloudfoundry.org/locket v0.0.0-20240521151413-b344fdd15d03 // indirect
	github.com/bmizerany/pat v0.0.0-20210406213842-e4b6760bdd6f // indirect
	github.com/census-instrumentation/opencensus-proto v0.4.1 // indirect
	github.com/cncf/xds/go v0.0.0-20240423153145-555b57ec207b // indirect
	github.com/cyphar/filepath-securejoin v0.2.5 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.0.4 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/go-test/deep v1.1.1 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/pprof v0.0.0-20240625030939-27f56978b8b0 // indirect
	github.com/jackc/pgx/v5 v5.6.0 // indirect
	github.com/openzipkin/zipkin-go v0.4.3 // indirect
	github.com/tedsuo/rata v1.0.0 // indirect
	golang.org/x/net v0.26.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	golang.org/x/tools v0.22.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240528184218-531527333157 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240701130421-f6361c86f094 // indirect
	google.golang.org/grpc v1.65.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
