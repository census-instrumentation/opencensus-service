module github.com/census-instrumentation/opencensus-service

require (
	cloud.google.com/go v0.37.1 // indirect
	contrib.go.opencensus.io/exporter/aws v0.0.0-20181029163544-2befc13012d0
	contrib.go.opencensus.io/exporter/ocagent v0.4.10
	contrib.go.opencensus.io/exporter/stackdriver v0.9.2
	github.com/Azure/azure-sdk-for-go v6.0.0-beta+incompatible // indirect
	github.com/Azure/go-autorest v11.4.0+incompatible // indirect
	github.com/DataDog/datadog-go v0.0.0-20180822151419-281ae9f2d895 // indirect
	github.com/DataDog/opencensus-go-exporter-datadog v0.0.0-20181026070331-e7c4bd17b329
	github.com/VividCortex/gohistogram v1.0.0 // indirect
	github.com/apache/thrift v0.0.0-20161221203622-b2a4d4ae21c7
	github.com/aws/aws-sdk-go v1.18.6 // indirect
	github.com/bmizerany/perks v0.0.0-20141205001514-d9a9656a3a4b // indirect
	github.com/census-instrumentation/opencensus-proto v0.2.0
	github.com/dgrijalva/jwt-go v3.2.0+incompatible // indirect
	github.com/facebookgo/ensure v0.0.0-20160127193407-b4ab57deab51 // indirect
	github.com/facebookgo/limitgroup v0.0.0-20150612190941-6abd8d71ec01 // indirect
	github.com/facebookgo/muster v0.0.0-20150708232844-fd3d7953fd52 // indirect
	github.com/facebookgo/stack v0.0.0-20160209184415-751773369052 // indirect
	github.com/facebookgo/subset v0.0.0-20150612182917-8dac2c3c4870 // indirect
	github.com/go-logfmt/logfmt v0.4.0 // indirect
	github.com/gogo/googleapis v1.1.0 // indirect
	github.com/golang/groupcache v0.0.0-20190129154638-5b532d6fd5ef // indirect
	github.com/golang/protobuf v1.3.1
	github.com/google/go-cmp v0.2.0
	github.com/google/gofuzz v0.0.0-20170612174753-24818f796faf // indirect
	github.com/googleapis/gnostic v0.2.0 // indirect
	github.com/gophercloud/gophercloud v0.0.0-20190206021053-df38e1611dbe // indirect
	github.com/gorilla/mux v1.6.2
	github.com/gregjones/httpcache v0.0.0-20190203031600-7a902570cb17 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.8.5
	github.com/hashicorp/consul v1.4.2 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.0 // indirect
	github.com/hashicorp/go-rootcerts v1.0.0 // indirect
	github.com/hashicorp/serf v0.8.2 // indirect
	github.com/honeycombio/libhoney-go v1.8.2 // indirect
	github.com/honeycombio/opencensus-exporter v0.0.0-20181101214123-9be2bb327b5a
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/jaegertracing/jaeger v1.9.0
	github.com/json-iterator/go v1.1.5 // indirect
	github.com/mitchellh/mapstructure v1.1.2 // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/omnition/scribe-go v0.0.0-20190131012523-9e3c68f31124
	github.com/opentracing/opentracing-go v1.0.2 // indirect
	github.com/openzipkin/zipkin-go v0.1.6
	github.com/orijtech/prometheus-go-metrics-exporter v0.0.3-0.20190313163149-b321c5297f60
	github.com/orijtech/promreceiver v0.0.6
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/philhofer/fwd v1.0.0 // indirect
	github.com/pkg/errors v0.8.0
	github.com/prashantv/protectmem v0.0.0-20171002184600-e20412882b3a // indirect
	github.com/prometheus/client_golang v0.9.3-0.20190127221311-3c4408c8b829
	github.com/prometheus/procfs v0.0.0-20190117184657-bf6a532e95b1
	github.com/prometheus/prometheus v0.0.0-20190131111325-62e591f928dd
	github.com/rs/cors v1.6.0
	github.com/samuel/go-zookeeper v0.0.0-20180130194729-c4fab1ac1bec // indirect
	github.com/soheilhy/cmux v0.1.4
	github.com/spf13/cast v1.2.0
	github.com/spf13/cobra v0.0.3
	github.com/spf13/viper v1.2.1
	github.com/streadway/quantile v0.0.0-20150917103942-b0c588724d25 // indirect
	github.com/tinylib/msgp v1.0.2 // indirect
	github.com/uber-go/atomic v1.3.2 // indirect
	github.com/uber/jaeger-client-go v2.15.0+incompatible // indirect
	github.com/uber/jaeger-lib v2.0.0+incompatible
	github.com/uber/tchannel-go v1.10.0
	github.com/yancl/opencensus-go-exporter-kafka v0.0.0-20181029030031-9c471c1bfbeb
	go.opencensus.io v0.19.3
	go.uber.org/atomic v1.3.2 // indirect
	go.uber.org/multierr v1.1.0 // indirect
	go.uber.org/zap v1.9.1
	golang.org/x/net v0.0.0-20190320064053-1272bf9dcd53 // indirect
	golang.org/x/oauth2 v0.0.0-20190319182350-c85d3e98c914 // indirect
	google.golang.org/api v0.2.0
	google.golang.org/grpc v1.19.1
	gopkg.in/DataDog/dd-trace-go.v1 v1.4.0 // indirect
	gopkg.in/alexcesaro/statsd.v2 v2.0.0 // indirect
	gopkg.in/fsnotify/fsnotify.v1 v1.4.7 // indirect
	gopkg.in/yaml.v2 v2.2.2
	k8s.io/apimachinery v0.0.0-20190207091153-095b9d203467 // indirect
	k8s.io/kube-openapi v0.0.0-20190205224424-fd29a9f2f429 // indirect
	sigs.k8s.io/structured-merge-diff v0.0.0-20190130003954-e5e029740eb8 // indirect
)
