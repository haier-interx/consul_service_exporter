package main

import (
	"fmt"
	"net/http"
	"net/url"
	//"sort"
	"strings"

	consul_api "github.com/hashicorp/consul/api"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	listenAddress  *string
	metricsPath    *string
	consulHost     *string
	consulServices *[]string
	serviceTags    *[]string
)

func init() {
	listenAddress = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9111").String()
	metricsPath = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
	consulHost = kingpin.Flag("consul.server", "HTTP API address of a Consul server or agent. (prefix with https:// to connect over HTTPS)").Default("http://localhost:8500").String()
	consulServices = kingpin.Flag("consul.service", "Consule service list").Strings()
	serviceTags = kingpin.Flag("consul.tag", "Consule service tag").Strings()

	kingpin.Parse()
}

func main() {
	e, err := newFirstExporter(*consulHost, *consulServices, *serviceTags)
	if err != nil {
		panic(err)
	}

	prometheus.MustRegister(e)
	http.Handle(*metricsPath, prometheus.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>Dial Check Exporter</title></head>
             <body>
             <h1>Consul Exporter</h1>
             <p><a href='` + *metricsPath + `'>Metrics</a></p>
             </body>
             </html>`))
	})

	log.Infoln("Listening on", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}

type FirstExporter struct {
	client   *consul_api.Client
	addr     string
	services map[string]bool
	tags     []string

	consul_desc *prometheus.Desc
	up_desc     *prometheus.Desc
	//exporter_hash map[string]*ServiceExporter
}

func (e *FirstExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.consul_desc
	ch <- e.up_desc
}
func (e *FirstExporter) Collect(ch chan<- prometheus.Metric) {
	srvs_map := e.services
	if len(e.services) == 0 {
		srvnames, _, err := e.client.Catalog().Services(&consul_api.QueryOptions{})
		if err != nil {
			ch <- prometheus.MustNewConstMetric(
				e.consul_desc, prometheus.GaugeValue, 0, e.addr,
			)
		} else {
			ch <- prometheus.MustNewConstMetric(
				e.consul_desc, prometheus.GaugeValue, 1, e.addr,
			)
		}
		for srv_name, _ := range srvnames {
			srvs_map[srv_name] = true
		}
	}

	for srv_name, _ := range srvs_map {
		// health service
		srvs, _, err := e.client.Health().Service(srv_name, "", false, new(consul_api.QueryOptions))
		if err != nil {
			log.Errorf("get health services %s failed: %v", srv_name, err)
			continue
		}
		//log.Infof("%+v", srvs)

		for _, srv_obj := range srvs {
			srv := srv_obj.Service

			label_values := make(map[string]string)
			for _, tag := range e.tags {
				label_values[tag] = ""
			}

			//log.Infof("get srv tags:%+v", srv.Tags)
			for _, tag := range srv.Tags {
				if tag == "" {
					continue
				}

				label_name_value := strings.SplitN(tag, "=", 2)
				if len(label_name_value) != 2 {
					log.Warnf("format tag failed: %s", tag)
					continue
				}
				label_value, found := label_values[label_name_value[0]]
				if !found {
					continue
				}

				if label_value != "" {
					log.Warnf("the tag %s has defined in srv %s", tag, srv.ID)
					continue
				}
				label_values[label_name_value[0]] = label_name_value[1]
			}

			ignore := false
			values := make([]string, 0, len(label_values)+6)
			values = append(values,
				srv_obj.Node.Datacenter, srv_obj.Node.Address,
				srv_name, srv.ID, srv.Address,
				fmt.Sprintf("%d", srv.Port),
			)
			for _, tag := range e.tags {
				if label_values[tag] == "" {
					log.Warnf("the %s of service(%s) %s is null, ignore it!", tag, srv_name, srv.ID)
					ignore = true
					break
				}
				values = append(values, label_values[tag])
			}
			if ignore {
				continue
			}
			//log.Infof("%+v", values)

			// check
			//log.Infof("%+v", srv_obj.Checks)
			for _, check := range srv_obj.Checks {
				if check.CheckID != "serfHealth" {
					if check.Status == "passing" {
						ch <- prometheus.MustNewConstMetric(
							e.up_desc, prometheus.GaugeValue, 1, values...,
						)
					} else {
						log.Warnf("%s %s: %s", check.CheckID, check.Status, check.Output)
						ch <- prometheus.MustNewConstMetric(
							e.up_desc, prometheus.GaugeValue, 0, values...,
						)
					}
					break
				}
			}

		}
	}

}
func newFirstExporter(addr string, services []string, tags []string) (*FirstExporter, error) {
	client, err := newConsulClient(addr)
	if err != nil {
		return nil, err
	}

	srvs_map := make(map[string]bool)
	for _, srv := range services {
		srvs_map[srv] = true
	}

	all_tags := make([]string, 0, len(tags)+6)
	all_tags = append(all_tags, "dc", "node_addr", "name", "id", "addr", "port")
	all_tags = append(all_tags, tags...)

	return &FirstExporter{
		client, addr,
		srvs_map,
		tags,
		prometheus.NewDesc(
			prometheus.BuildFQName("dial", "", "consul"),
			"consul state",
			[]string{"addr"}, nil,
		),
		prometheus.NewDesc(
			prometheus.BuildFQName("dial", "", "up"),
			"consul state",
			all_tags, nil,
		),
		//make(map[string]*ServiceExporter),
	}, nil
}

func newConsulClient(uri string) (*consul_api.Client, error) {
	if !strings.Contains(uri, "://") {
		uri = "http://" + uri
	}
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid consul URL: %s", err)
	}

	config := consul_api.DefaultConfig()
	config.Address = u.Host
	config.Scheme = u.Scheme

	return consul_api.NewClient(config)
}

type ServiceExporter struct {
	Desc           *prometheus.Desc
	variableLabels []string
	metric_chan    chan prometheus.Metric
}

func (se *ServiceExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- se.Desc
}
func (se *ServiceExporter) Collect(ch chan<- prometheus.Metric) {

}
func NewServiceExporter(tags []string) *ServiceExporter {
	return &ServiceExporter{
		Desc: prometheus.NewDesc(
			prometheus.BuildFQName("dial", "", strings.Join(tags, "_")),
			"Dial check service in consul for tag: "+strings.Join(tags, ","),
			tags, nil,
		),
		variableLabels: tags,
		metric_chan:    make(chan prometheus.Metric),
	}
}