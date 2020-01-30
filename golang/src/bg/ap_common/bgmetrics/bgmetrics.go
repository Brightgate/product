/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package bgmetrics

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"bg/common/cfgapi"
)

// Counter is used to capture stats that can only increase - generally one step
// at a time
type Counter struct {
	updated bool
	val     int64
}

// Gauge is used to capture metrics that can swing back and forth within a
// range, where we only care about the current value
type Gauge struct {
	updated bool
	val     float64
}

// Summary is used to capture metrics that can swing back and forth within a
// range, where we care about the history, trends, and frequencies of those
// values.
type Summary struct {
	updated bool
	// Will eventually be a bucketed slice
	val float64
}

// DurationSummary is similar to a Summary metric, but is specifically used to
// track time.Duration values.
type DurationSummary struct {
	updated bool
	// Will eventually be a bucketed slice
	val time.Duration
}

// Metrics is an opaque handle used to register, update, and track a set of
// metrics.
type Metrics struct {
	pname   string
	cfgRoot string
	config  *cfgapi.Handle

	updateFreq time.Duration
	updateLast time.Time
	updateChan chan bool

	counters  map[string]*Counter
	gauges    map[string]*Gauge
	summaries map[string]*Summary
	durations map[string]*DurationSummary

	sync.Mutex
}

// Add increases a Counter by an arbitrary amount
func (c *Counter) Add(val int64) {
	c.val += val
	c.updated = true
}

// Inc increases a Counter by 1
func (c *Counter) Inc() {
	c.val++
	c.updated = true
}

// Reset sets a counter to 0
func (c *Counter) Reset() {
	c.val = 0
	c.updated = true
}

// NewCounter allocates and returns a new Counter metric
func (m *Metrics) NewCounter(name string) *Counter {
	m.Lock()
	defer m.Unlock()

	m.addMetricProperty(name)
	c := &Counter{}
	m.counters[name] = c
	return c
}

// Set sets a gauge metric to a new value
func (g *Gauge) Set(val float64) {
	g.val = val
	g.updated = true
}

// NewGauge allocates and returns a new Gauge metric
func (m *Metrics) NewGauge(name string) *Gauge {
	m.Lock()
	defer m.Unlock()

	m.addMetricProperty(name)
	g := &Gauge{}
	m.gauges[name] = g
	return g
}

// Observe adds a new duration to the set of durations the given metric has
// taken on.
func (d *DurationSummary) Observe(val time.Duration) {
	d.val = val
	d.updated = true
}

// NewDurationSummary allocates and returns a new DurationSummary metric
func (m *Metrics) NewDurationSummary(name string) *DurationSummary {
	m.Lock()
	defer m.Unlock()

	m.addMetricProperty(name)
	d := &DurationSummary{}
	m.durations[name] = d
	return d
}

// Reset sets the current duration to 0 and erases any history.
func (d *DurationSummary) Reset() {
	d.val = time.Duration(0)
	d.updated = true
}

// Observe adds a new value to the set of values the given metric has taken on.
func (s *Summary) Observe(val float64) {
	s.val = val
	s.updated = true
}

// NewSummary allocates and returns a new Summary metric
func (m *Metrics) NewSummary(name string) *Summary {
	m.Lock()
	defer m.Unlock()

	m.addMetricProperty(name)
	s := &Summary{}
	m.summaries[name] = s
	return s
}

// Reset sets the current value to 0 and erases any history.
func (s *Summary) Reset() {
	s.val = 0
	s.updated = true
}

func (m *Metrics) addMetricProperty(name string) {
	if m.config == nil {
		return
	}

	path := m.cfgRoot + "/" + name
	if err := m.config.AddPropValidation(path, "string"); err != nil {
		log.Fatalf("failed to add metrics path %s: %v", path, err)
	}
}

// Dump is a debugging function which prints out all of the current metrics and
// their values.
func (m *Metrics) Dump() {
	m.Lock()
	defer m.Unlock()

	for name, c := range m.counters {
		fmt.Printf("  %s: %d\n", name, c.val)
	}
	for name, g := range m.gauges {
		fmt.Printf("  %s: %1.2f\n", name, g.val)
	}
	for name, s := range m.summaries {
		fmt.Printf("  %s: %1.2f\n", name, s.val)
	}
}

func (m *Metrics) addOp(name, val string) cfgapi.PropertyOp {
	return cfgapi.PropertyOp{
		Op:    cfgapi.PropCreate,
		Name:  m.cfgRoot + "/" + name,
		Value: val,
	}
}

func (m *Metrics) addFloatOp(name string, val float64) cfgapi.PropertyOp {
	var s string

	if val < 1000 {
		s = strconv.FormatFloat(val, 'f', 4, 64)
	} else if val < 10000000 {
		s = strconv.FormatFloat(val, 'f', 0, 64)
	} else {
		s = strconv.FormatFloat(val, 'g', 8, 64)
	}
	s = strings.TrimRight(s, ".0")

	return m.addOp(name, s)
}

func (m *Metrics) addIntOp(name string, val int64) cfgapi.PropertyOp {
	s := strconv.FormatInt(val, 10)
	return m.addOp(name, s)
}

func (m *Metrics) addDurationOp(name string, val time.Duration) cfgapi.PropertyOp {
	s := val.String()
	return m.addOp(name, s)
}

// PushUpdates sends any changes in our set of metrics to ap.configd, where they
// get stored at @/metrics/<daemon>/<metric>
func (m *Metrics) PushUpdates() {
	if m.config == nil {
		return
	}

	m.Lock()
	defer m.Unlock()

	ops := make([]cfgapi.PropertyOp, 0)
	for name, c := range m.counters {
		if c.updated {
			c.updated = false
			ops = append(ops, m.addIntOp(name, c.val))
		}
	}
	for name, g := range m.gauges {
		if g.updated {
			g.updated = false
			ops = append(ops, m.addFloatOp(name, g.val))
		}
	}
	for name, s := range m.summaries {
		if s.updated {
			s.updated = false
			ops = append(ops, m.addFloatOp(name, s.val))
		}
	}
	for name, d := range m.durations {
		if d.updated {
			d.updated = false
			ops = append(ops, m.addDurationOp(name, d.val))
		}
	}
	if len(ops) > 0 {
		if _, err := m.config.Execute(nil, ops).Wait(nil); err != nil {
			fmt.Printf("Error updating metrics: %v", err)
		}
	}
}

// UpdateFrequency changes the frequency with which updates are pushed to
// ap.configd.
func (m *Metrics) UpdateFrequency(d time.Duration) {
	m.updateFreq = d

	nextUpdate := m.updateLast.Add(m.updateFreq)
	delta := nextUpdate.Sub(time.Now())
	if delta > 0 {
		time.AfterFunc(delta, func() { m.updateChan <- true })
	} else {
		m.updateChan <- true
	}
}

func (m *Metrics) updateLoop() {
	for {
		nextUpdate := m.updateLast.Add(m.updateFreq)

		if n := time.Now(); n.After(nextUpdate) {
			m.updateLast = n
			m.PushUpdates()
			nextUpdate = n.Add(m.updateFreq)
		}

		delta := nextUpdate.Sub(time.Now())
		if delta > 0 {
			time.AfterFunc(delta, func() { m.updateChan <- true })

			<-m.updateChan
		}
	}
}

// NewMetrics allocates a new bgmetrics handle
func NewMetrics(pname string, config *cfgapi.Handle) *Metrics {
	defaultFreq := 5 * time.Second

	m := &Metrics{
		pname:      pname,
		cfgRoot:    "@/metrics/daemons/" + pname,
		config:     config,
		updateFreq: defaultFreq,
		updateChan: make(chan bool, 1),
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		summaries:  make(map[string]*Summary),
		durations:  make(map[string]*DurationSummary),
	}

	go m.updateLoop()
	return m
}
