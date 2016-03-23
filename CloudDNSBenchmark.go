package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net"
	"sort"
	"time"

	"github.com/miekg/dns"
)

var (
	numOfQueries = flag.Int("q", 20, "Number of domains to test max is 200")
)

type result struct {
	server string
	host   string
	rtt    time.Duration
	errors int
	ok     bool
}

func main() {

	flag.Parse()
	if *numOfQueries > 200 {
		*numOfQueries = 200
	}
	fmt.Printf("Starting CloudDNS Benchmarks, using %d random domains\n", *numOfQueries)

	config := new(dns.ClientConfig)

	config.Port = "53"
	config.Ndots = 1
	config.Timeout = 20
	config.Attempts = 5

	var hosts []string
	longesthostname := 0

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := 0; i < *numOfQueries; i++ {
		host := Hosts[r.Intn(len(Hosts))]
		// fmt.Println(host)
		hosts = append(hosts, host)
		if len(host) > longesthostname {
			longesthostname = len(host)
		}
	}

	longestservername := 0
	for i := 0; i < len(servers); i++ {
		if len(servers[i]) > longestservername {
			longestservername = len(servers[i])
		}
		config.Servers = append(config.Servers, servers[i])
	}

	for i := 0; i < len(servers); i++ {
		for j := len(servers[i]); j < longestservername; j++ {
			servers[i] = servers[i] + " "
		}
	}

	// config, _ := dns.ClientConfigFromFile("/etc/resolv.conf")
	c := new(dns.Client)

	m := new(dns.Msg)

	var results []result
	for _, host := range hosts {
		m.SetQuestion(dns.Fqdn(host), dns.TypeA)
		m.RecursionDesired = true

		for i := 0; i < len(servers); i++ {
			var res result
			retries := 0
		retry:
			r, rtt, err := c.Exchange(m, net.JoinHostPort(config.Servers[i], config.Port))
			if r == nil {
				fmt.Printf("Host %s DNS server %s error: [%v]\n", host, servers[i], err)
				retries++
				res.errors = retries
				if retries < config.Attempts {
					goto retry
				} else {
					res.ok = false
					goto end

				}
			}
			fmt.Printf("Host %s DNS server %s query time: [%5v]ms\n", host, servers[i], rtt.Nanoseconds()/1e6)

			res.server = config.Servers[i]
			res.host = host
			res.rtt = rtt
			res.ok = true
			results = append(results, res)
		end:
		}
	}

	generateReport(results)

}

type roundTrip struct {
	// in milliseconds
	min float64
	max float64
	avg float64
	std float64
}

type Times []time.Duration

type record struct {
	server string
	times  roundTrip
}

type Report []record

func generateReport(results []result) {
	s := make(map[string]Times)
	for _, v := range results {
		if v.ok {
			s[v.server] = append(s[v.server], v.rtt)
		}
	}
	var report Report
	for k, v := range s {
		times := calcRoundTrip(v)
		// fmt.Printf("%15v    %v\n", k, times)
		var r record
		r.server = k
		r.times = times
		report = append(report, r)
	}
	sort.Sort(report)

	fmt.Println("Results, Ordered by lowest average response time")

	for _, v := range report {
		fmt.Printf("%15v    %v\n", v.server, v.times)
	}

}

func (r Report) Len() int {
	return len(r)
}

func (r Report) Less(i, j int) bool {
	return r[i].times.avg < r[j].times.avg
}

func (r Report) Swap(i, j int) {
	r[i], r[j] = r[j], r[i]
}

func calcRoundTrip(times Times) roundTrip {

	var r roundTrip
	r.avg = avg(times)
	r.min = min(times)
	r.max = max(times)
	r.std = std(times)
	return r

}

func (r roundTrip) String() string {

	return fmt.Sprintf("min[%5.1f]ms max[%5.1f]ms avg[%5.1f]ms std[%5.1f]ms ", r.min, r.max, r.avg, r.std)
}

func min(times Times) float64 {

	min := 1e6
	for i := 0; i < len(times); i++ {
		t := float64(times[i].Nanoseconds() / 1e6)
		if t < min {
			min = t
		}
	}
	return min
}

func max(times Times) float64 {
	max := 0.0
	for i := 0; i < len(times); i++ {
		t := float64(times[i].Nanoseconds() / 1e6)
		if t > max {
			max = t
		}
	}
	return max
}

func avg(times Times) float64 {
	avg := 0.0
	for i := 0; i < len(times); i++ {
		t := float64(times[i].Nanoseconds() / 1e6)
		avg += t
	}
	return avg / float64(len(times))
}

func std(times Times) float64 {
	size := len(times)
	var nums []float64
	var numMean float64
	for i := 0; i < size; i++ {
		nums = append(nums, float64(times[i].Nanoseconds()/1e6))
		numMean += nums[i]
	}
	numMean = numMean / float64(size)

	var newnumMean float64
	for i := 0; i < size; i++ {
		nums[i] = nums[i] - numMean
		nums[i] *= nums[i]
		newnumMean += nums[i]
	}
	newnumMean = newnumMean / float64(size)
	std := math.Sqrt(newnumMean)

	return std
}
