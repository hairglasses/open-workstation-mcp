package process

import (
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/hairglasses-studio/mcpkit/handler"
)

type DbusProbeInput struct {
	Bus       string   `json:"bus,omitempty" jsonschema:"description=D-Bus bus to query: session (default) or system"`
	Probe     string   `json:"probe" jsonschema:"required,description=Probe type: unit_state\\, properties\\, daemon_lifecycle\\, list_units\\, or lifecycle_events"`
	Unit      string   `json:"unit,omitempty" jsonschema:"description=Systemd unit name (required for unit_state and unit properties)"`
	Interface string   `json:"interface,omitempty" jsonschema:"description=D-Bus interface name for properties query"`
	Property  string   `json:"property,omitempty" jsonschema:"description=Specific property name to query. If empty\\, queries all properties."`
	Patterns  []string `json:"patterns,omitempty" jsonschema:"description=Unit patterns for list_units (e.g. [*.service])"`
	TimeoutMs int      `json:"timeout_ms,omitempty" jsonschema:"description=Probe timeout in milliseconds. Default 5000."`
}

type DbusUnitState struct {
	Unit                       string `json:"unit"`
	ActiveState                string `json:"active_state"`
	SubState                   string `json:"sub_state"`
	LoadState                  string `json:"load_state"`
	UnitFileState              string `json:"unit_file_state,omitempty"`
	Description                string `json:"description"`
	MainPID                    int    `json:"main_pid,omitempty"`
	MemoryCurrentBytes         uint64 `json:"memory_current_bytes,omitempty"`
	CPUUsageNSec               uint64 `json:"cpu_usage_nsec,omitempty"`
	ActiveEnterTimestampUSec   uint64 `json:"active_enter_timestamp_usec,omitempty"`
	InactiveEnterTimestampUSec uint64 `json:"inactive_enter_timestamp_usec,omitempty"`
	RestartCount               uint32 `json:"restart_count,omitempty"`
}

type DbusDaemonLifecycle struct {
	Version        string  `json:"version"`
	Features       string  `json:"features"`
	Architecture   string  `json:"architecture"`
	Tainted        string  `json:"tainted"`
	SystemState    string  `json:"system_state"`
	Virtualization string  `json:"virtualization"`
	Progress       float64 `json:"progress"`
	UnitsLoaded    int     `json:"units_loaded"`
	JobsQueued     int     `json:"jobs_queued"`
	ControlGroup   string  `json:"control_group,omitempty"`
}

type DbusUnitSummary struct {
	Unit        string `json:"unit"`
	Description string `json:"description"`
	LoadState   string `json:"load_state"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state"`
	ObjectPath  string `json:"object_path,omitempty"`
}

type DbusEvent struct {
	Sender    string `json:"sender"`
	Path      string `json:"path"`
	Interface string `json:"interface"`
	Member    string `json:"member"`
	Timestamp string `json:"timestamp"`
}

type DbusProbeOutput struct {
	Bus             string               `json:"bus"`
	Probe           string               `json:"probe"`
	Success         bool                 `json:"success"`
	Unit            string               `json:"unit,omitempty"`
	UnitState       *DbusUnitState       `json:"unit_state,omitempty"`
	Properties      map[string]any       `json:"properties,omitempty"`
	DaemonLifecycle *DbusDaemonLifecycle `json:"daemon_lifecycle,omitempty"`
	Units           []DbusUnitSummary    `json:"units,omitempty"`
	Events          []DbusEvent          `json:"events,omitempty"`
	Error           string               `json:"error,omitempty"`
}

type rawUnitInfo struct {
	Name        string
	Description string
	LoadState   string
	ActiveState string
	SubState    string
	Following   string
	UnitPath    dbus.ObjectPath
	JobID       uint32
	JobType     string
	JobPath     dbus.ObjectPath
}

func connectDbus(busName string) (*dbus.Conn, string, error) {
	busLower := strings.ToLower(strings.TrimSpace(busName))
	switch busLower {
	case "system":
		conn, err := dbus.ConnectSystemBus()
		return conn, "system", err
	case "session":
		conn, err := dbus.ConnectSessionBus()
		if err == nil {
			return conn, "session", nil
		}
		sysConn, sysErr := dbus.ConnectSystemBus()
		if sysErr == nil {
			return sysConn, "system (session unavailable)", nil
		}
		return nil, "session", err
	default:
		conn, err := dbus.ConnectSessionBus()
		if err == nil {
			return conn, "session", nil
		}
		sysConn, sysErr := dbus.ConnectSystemBus()
		if sysErr == nil {
			return sysConn, "system (fallback)", nil
		}
		return nil, "none", err
	}
}

func normalizeVariantValue(val any) any {
	if v, ok := val.(dbus.Variant); ok {
		return normalizeVariantValue(v.Value())
	}
	switch x := val.(type) {
	case dbus.ObjectPath:
		return string(x)
	case dbus.Signature:
		return fmt.Sprint(x)
	case []dbus.Variant:
		res := make([]any, len(x))
		for i, item := range x {
			res[i] = normalizeVariantValue(item)
		}
		return res
	case map[string]dbus.Variant:
		res := make(map[string]any, len(x))
		for k, item := range x {
			res[k] = normalizeVariantValue(item)
		}
		return res
	default:
		return val
	}
}

func getPropStr(props map[string]dbus.Variant, key string) string {
	if v, ok := props[key]; ok {
		if s, ok := v.Value().(string); ok {
			return s
		}
	}
	return ""
}

func getPropUint32(props map[string]dbus.Variant, key string) uint32 {
	if v, ok := props[key]; ok {
		if u, ok := v.Value().(uint32); ok {
			return u
		}
		if i, ok := v.Value().(int32); ok {
			return uint32(i)
		}
	}
	return 0
}

func getPropUint64(props map[string]dbus.Variant, key string) uint64 {
	if v, ok := props[key]; ok {
		if u, ok := v.Value().(uint64); ok {
			if u == ^uint64(0) {
				return 0
			}
			return u
		}
		if i, ok := v.Value().(int64); ok {
			return uint64(i)
		}
	}
	return 0
}

func getPropFloat64(props map[string]dbus.Variant, key string) float64 {
	if v, ok := props[key]; ok {
		if f, ok := v.Value().(float64); ok {
			return f
		}
		if d, ok := v.Value().(float32); ok {
			return float64(d)
		}
	}
	return 0.0
}

func probeUnitState(conn *dbus.Conn, unitName string) (*DbusUnitState, error) {
	mgr := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	var unitPath dbus.ObjectPath
	err := mgr.Call("org.freedesktop.systemd1.Manager.GetUnit", 0, unitName).Store(&unitPath)
	if err != nil {
		err2 := mgr.Call("org.freedesktop.systemd1.Manager.LoadUnit", 0, unitName).Store(&unitPath)
		if err2 != nil {
			return nil, fmt.Errorf("GetUnit/LoadUnit %q: %w", unitName, err)
		}
	}

	unitObj := conn.Object("org.freedesktop.systemd1", unitPath)
	var unitProps map[string]dbus.Variant
	err = unitObj.Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.systemd1.Unit").Store(&unitProps)
	if err != nil {
		return nil, fmt.Errorf("GetAll Unit properties %q: %w", unitName, err)
	}

	var svcProps map[string]dbus.Variant
	_ = unitObj.Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.systemd1.Service").Store(&svcProps)

	state := &DbusUnitState{
		Unit:                       unitName,
		ActiveState:                getPropStr(unitProps, "ActiveState"),
		SubState:                   getPropStr(unitProps, "SubState"),
		LoadState:                  getPropStr(unitProps, "LoadState"),
		UnitFileState:              getPropStr(unitProps, "UnitFileState"),
		Description:                getPropStr(unitProps, "Description"),
		ActiveEnterTimestampUSec:   getPropUint64(unitProps, "ActiveEnterTimestamp"),
		InactiveEnterTimestampUSec: getPropUint64(unitProps, "InactiveEnterTimestamp"),
	}

	if svcProps != nil {
		state.MainPID = int(getPropUint32(svcProps, "MainPID"))
		state.MemoryCurrentBytes = getPropUint64(svcProps, "MemoryCurrent")
		state.CPUUsageNSec = getPropUint64(svcProps, "CPUUsageNSec")
		state.RestartCount = getPropUint32(svcProps, "NRestarts")
	}

	return state, nil
}

func probeDaemonLifecycle(conn *dbus.Conn) (*DbusDaemonLifecycle, map[string]any, error) {
	mgr := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	var rawProps map[string]dbus.Variant
	err := mgr.Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.systemd1.Manager").Store(&rawProps)
	if err != nil {
		return nil, nil, fmt.Errorf("GetAll Manager properties: %w", err)
	}

	norm := make(map[string]any, len(rawProps))
	for k, v := range rawProps {
		norm[k] = normalizeVariantValue(v)
	}

	lifecycle := &DbusDaemonLifecycle{
		Version:        getPropStr(rawProps, "Version"),
		Features:       getPropStr(rawProps, "Features"),
		Architecture:   getPropStr(rawProps, "Architecture"),
		Tainted:        getPropStr(rawProps, "Tainted"),
		SystemState:    getPropStr(rawProps, "SystemState"),
		Virtualization: getPropStr(rawProps, "Virtualization"),
		Progress:       getPropFloat64(rawProps, "Progress"),
		UnitsLoaded:    int(getPropUint32(rawProps, "NNames")),
		JobsQueued:     int(getPropUint32(rawProps, "NJobs")),
		ControlGroup:   getPropStr(rawProps, "ControlGroup"),
	}

	return lifecycle, norm, nil
}

func probeListUnits(conn *dbus.Conn, patterns []string) ([]DbusUnitSummary, error) {
	mgr := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	var raw []rawUnitInfo
	var err error

	if len(patterns) > 0 {
		err = mgr.Call("org.freedesktop.systemd1.Manager.ListUnitsByPatterns", 0, []string{}, patterns).Store(&raw)
	}
	if err != nil || len(patterns) == 0 {
		err = mgr.Call("org.freedesktop.systemd1.Manager.ListUnits", 0).Store(&raw)
	}
	if err != nil {
		return nil, fmt.Errorf("ListUnits: %w", err)
	}

	summaries := make([]DbusUnitSummary, 0, len(raw))
	for _, r := range raw {
		summaries = append(summaries, DbusUnitSummary{
			Unit:        r.Name,
			Description: r.Description,
			LoadState:   r.LoadState,
			ActiveState: r.ActiveState,
			SubState:    r.SubState,
			ObjectPath:  string(r.UnitPath),
		})
	}
	return summaries, nil
}

func probeProperties(conn *dbus.Conn, unitName, iface, prop string) (map[string]any, error) {
	var obj dbus.BusObject
	if unitName != "" {
		mgr := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		var unitPath dbus.ObjectPath
		err := mgr.Call("org.freedesktop.systemd1.Manager.GetUnit", 0, unitName).Store(&unitPath)
		if err != nil {
			err2 := mgr.Call("org.freedesktop.systemd1.Manager.LoadUnit", 0, unitName).Store(&unitPath)
			if err2 != nil {
				return nil, fmt.Errorf("GetUnit/LoadUnit %q: %w", unitName, err)
			}
		}
		obj = conn.Object("org.freedesktop.systemd1", unitPath)
		if iface == "" {
			iface = "org.freedesktop.systemd1.Unit"
		}
	} else {
		obj = conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		if iface == "" {
			iface = "org.freedesktop.systemd1.Manager"
		}
	}

	res := make(map[string]any)
	if prop != "" {
		var v dbus.Variant
		err := obj.Call("org.freedesktop.DBus.Properties.Get", 0, iface, prop).Store(&v)
		if err != nil {
			return nil, fmt.Errorf("Get property %s.%s: %w", iface, prop, err)
		}
		res[prop] = normalizeVariantValue(v)
		return res, nil
	}

	var rawProps map[string]dbus.Variant
	err := obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, iface).Store(&rawProps)
	if err != nil {
		return nil, fmt.Errorf("GetAll %s: %w", iface, err)
	}
	for k, v := range rawProps {
		res[k] = normalizeVariantValue(v)
	}
	return res, nil
}

func probeEvents(conn *dbus.Conn, timeoutMs int) ([]DbusEvent, error) {
	if timeoutMs <= 0 {
		timeoutMs = 500
	}
	if timeoutMs > 3000 {
		timeoutMs = 3000
	}

	call := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, "type='signal',sender='org.freedesktop.systemd1'")
	if call.Err != nil {
		return nil, fmt.Errorf("AddMatch signal: %w", call.Err)
	}

	ch := make(chan *dbus.Signal, 20)
	conn.Signal(ch)
	defer conn.RemoveSignal(ch)

	timer := time.NewTimer(time.Duration(timeoutMs) * time.Millisecond)
	defer timer.Stop()

	var events []DbusEvent
	for {
		select {
		case sig, ok := <-ch:
			if !ok {
				return events, nil
			}
			events = append(events, DbusEvent{
				Sender:    sig.Sender,
				Path:      string(sig.Path),
				Interface: sig.Name[:strings.LastIndex(sig.Name, ".")],
				Member:    sig.Name[strings.LastIndex(sig.Name, ".")+1:],
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			})
			if len(events) >= 50 {
				return events, nil
			}
		case <-timer.C:
			return events, nil
		}
	}
}

func ProbeDbus(input DbusProbeInput) (DbusProbeOutput, error) {
	probeType := strings.ToLower(strings.TrimSpace(input.Probe))
	if probeType == "" {
		return DbusProbeOutput{}, fmt.Errorf("[%s] probe type is required (unit_state, properties, daemon_lifecycle, list_units, or lifecycle_events)", handler.ErrInvalidParam)
	}

	conn, busUsed, err := connectDbus(input.Bus)
	if err != nil {
		return DbusProbeOutput{
			Bus:     busUsed,
			Probe:   probeType,
			Success: false,
			Error:   fmt.Sprintf("[%s] failed to connect to D-Bus (%s): %v", handler.ErrClientInit, input.Bus, err),
		}, nil
	}
	defer conn.Close()

	output := DbusProbeOutput{
		Bus:     busUsed,
		Probe:   probeType,
		Success: true,
		Unit:    input.Unit,
	}

	switch probeType {
	case "unit_state":
		if input.Unit == "" {
			return DbusProbeOutput{}, fmt.Errorf("[%s] unit is required for unit_state probe", handler.ErrInvalidParam)
		}
		state, err := probeUnitState(conn, input.Unit)
		if err != nil {
			output.Success = false
			output.Error = err.Error()
			return output, nil
		}
		output.UnitState = state

	case "properties":
		props, err := probeProperties(conn, input.Unit, input.Interface, input.Property)
		if err != nil {
			output.Success = false
			output.Error = err.Error()
			return output, nil
		}
		output.Properties = props

	case "daemon_lifecycle":
		lifecycle, props, err := probeDaemonLifecycle(conn)
		if err != nil {
			output.Success = false
			output.Error = err.Error()
			return output, nil
		}
		output.DaemonLifecycle = lifecycle
		output.Properties = props

	case "list_units":
		units, err := probeListUnits(conn, input.Patterns)
		if err != nil {
			output.Success = false
			output.Error = err.Error()
			return output, nil
		}
		output.Units = units

	case "lifecycle_events", "property_changes", "events":
		events, err := probeEvents(conn, input.TimeoutMs)
		if err != nil {
			output.Success = false
			output.Error = err.Error()
			return output, nil
		}
		output.Events = events

	default:
		return DbusProbeOutput{}, fmt.Errorf("[%s] unsupported probe type %q", handler.ErrInvalidParam, probeType)
	}

	return output, nil
}
