// CloudDNSBenchmark
// Copyright (C) 2016 Josh Gardiner

// This program is free software; you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation; either version 2 of the License, or
// (at your option) any later version.

// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.

// You should have received a copy of the GNU General Public License along
// with this program; if not, write to the Free Software Foundation, Inc.,
// 51 Franklin Street, Fifth Floor, Boston, MA 02110-1301 USA.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/miekg/dns"
)

var (
	numOfQueries  = flag.Int("q", 20, "Number of domains to test max is 200")
	numOResolvers = flag.Int("r", 40, "Number of simutanious resolvers")
	typeofHost    = flag.String("type", "t", "use top domains or others")
)

func main() {

	flag.Parse()

	fmt.Println("\nCloudDNSBenchmark version 0.0.3, Copyright (C) 2016 Josh Gardiner")
	fmt.Println("CloudDNSBenchmark comes with ABSOLUTELY NO WARRANTY;")
	fmt.Println("This is free software, and you are welcome to redistribute it")
	fmt.Println("under certain conditions;")
	fmt.Printf("\n\nStarting CloudDNS Benchmarks, using %d random domains\n", *numOfQueries)

	results := generator()

	generateReport(results)

	fmt.Print("\nPress ENTER to exit \n")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
		if scanner.Text() == "" {
			fmt.Println("exiting")
			os.Exit(0)
		}
	}

}

func generator() []Result {
	var results []Result
	var hosts List
	if *typeofHost == "top" {
		hosts = Top.randomSelect(*numOfQueries)
	} else {
		hosts = Hosts.randomSelect(*numOfQueries)
	}

	cloudResults := make(chan Result)
	cloudQuries := buildCloudQuries(hosts, Servers, cloudResults)
	var wg sync.WaitGroup
	// var localwg sync.WaitGroup
	wg.Add(len(cloudQuries))

	for _, v := range cloudQuries {
		go dnsworker(v)
	}

	queueLength := len(cloudQuries)
	queuePosition := 0
	for i := 0; i < *numOResolvers; i++ {
		if queuePosition < queueLength {
			cloudQuries[queuePosition].wait <- false
			queuePosition++
		}
	}

	localResults := make(chan Result)
	localQuries := buildLocalQuries(hosts, localResults)

	localQueueLength := len(localQuries)
	localQueuePosition := 0

	for _, v := range localQuries {
		go localLookup(v)
	}

	// Running Cloud DNS
	go func() {
		for {
			select {
			case r := <-cloudResults:
				fmt.Println(r)
				if queuePosition < queueLength {
					cloudQuries[queuePosition].wait <- false
					queuePosition++
				}
				results = append(results, r)
				wg.Done()
			case l := <-localResults:
				fmt.Println(l)
				if localQueuePosition < localQueueLength {
					localQuries[localQueuePosition].wait <- false
					localQueuePosition++
				}
				results = append(results, l)
				wg.Done()
			}
		}
	}()

	wg.Wait()

	// Running Local Resolver
	fmt.Println("Now Running Local")
	wg.Add(len(localQuries))

	for i := 0; i < 3; i++ {
		if localQueuePosition < localQueueLength {
			localQuries[localQueuePosition].wait <- false
			localQueuePosition++
		}
	}

	wg.Wait()
	return results

}

func buildCloudQuries(hosts, servers []string, resp chan Result) []Query {
	var quries []Query
	for i := range hosts {
		for j := range servers {
			var q Query
			q.wait = make(chan bool)
			q.result = resp
			q.host = hosts[i]
			q.server = servers[j]
			quries = append(quries, q)
		}
	}
	return quries
}

func buildLocalQuries(hosts []string, resp chan Result) []Query {
	var quries []Query
	for i := range hosts {
		var q Query
		q.wait = make(chan bool)
		q.result = resp
		q.host = hosts[i]
		quries = append(quries, q)
	}
	return quries
}

type List []string

func (l List) randomSelect(num int) []string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	var items []string

	for i := 0; i < num; i++ {
		item := l[r.Intn(len(l))]
		items = append(items, item)
	}
	return items
}

type Result struct {
	server string
	host   string
	rtt    time.Duration
	errors int
	ok     bool
}

func (r Result) String() string {
	if r.ok {
		return fmt.Sprintf("server: %15v host: %31v, time [%6v]ms", r.server, r.host, r.rtt.Nanoseconds()/1e6)
	} else {
		return fmt.Sprintf("server: %15v host: %31v, error [timeout]", r.server, r.host)
	}
}

type Query struct {
	server string
	host   string
	wait   chan bool
	result chan Result
}

type Querier func(Query)

func dnsworker(query Query) {
	<-query.wait
	config := new(dns.ClientConfig)
	config.Port = "53"
	config.Ndots = 1
	config.Timeout = 60
	config.Attempts = 5

	var r Result
	r.server = query.server
	r.host = query.host
	r.ok = false

	c := new(dns.Client)
	c.DialTimeout = 8 * time.Second
	c.ReadTimeout = 8 * time.Second
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(query.host), dns.TypeA)
	m.RecursionDesired = true

	for !r.ok {
		ans, rtt, _ := c.Exchange(m, net.JoinHostPort(query.server, config.Port))
		if ans == nil {
			// fmt.Printf("Host %s DNS server %s error: [%v]\n", query.host, query.server, err)
			r.errors++
			if r.errors > config.Attempts {
				query.result <- r
				return
			}
		} else {
			r.rtt = rtt
			r.ok = true
		}
	}
	query.result <- r
}

func localLookup(query Query) {
	<-query.wait
	var r Result
	r.host = query.host
	r.server = "Current DNS"
	r.ok = true
	var retries int
	var lookuptime time.Duration

	for retries = 0; retries < 5; retries++ {
		start := time.Now()
		_, err := net.LookupHost(r.host)
		end := time.Now()
		lookuptime = end.Sub(start)
		if err == nil {
			r.rtt = lookuptime
			query.result <- r
			return
		}
	}
	lookuptime = 0
	r.errors = retries
	r.ok = false

	query.result <- r

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
	errors int
}

type Report []record

func generateReport(results []Result) {
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

	fmt.Println("\n\nResults; Ordered by lowest average response time")

	for k, v := range report {
		fmt.Printf("#%2d %15v %v\n", k+1, v.server, v.times)
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

	return fmt.Sprintf("min[%6.1f]ms max[%6.1f]ms avg[%6.1f]ms jitter[%6.1f]ms ", r.min, r.max, r.avg, r.std)
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
