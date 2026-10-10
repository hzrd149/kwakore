//go:build linux

package gamepad

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"time"

	"github.com/ebitengine/purego"
)

// SDL owns discovery and controller mappings. Only its controller subsystem
// is initialized: the daemon needs no display, audio device or native window.
// Dynamic loading keeps installs without SDL usable for other napplets.
var native struct {
	once               sync.Once
	err                error
	init               func(uint32) int32
	quit               func(uint32)
	hint               func(string, string) int32
	update             func()
	eventState         func(int32) int32
	joystickEvents     func(int32) int32
	count              func() int32
	instanceID         func(int32) int32
	isController       func(int32) int32
	controllerOpen     func(int32) uintptr
	controllerClose    func(uintptr)
	controllerJoystick func(uintptr) uintptr
	controllerAxis     func(uintptr, int32) int16
	controllerButton   func(uintptr, int32) uint8
	joystickOpen       func(int32) uintptr
	joystickClose      func(uintptr)
	joystickName       func(uintptr) string
	vendor             func(uintptr) uint16
	product            func(uintptr) uint16
	attached           func(uintptr) int32
	numAxes            func(uintptr) int32
	numButtons         func(uintptr) int32
	numHats            func(uintptr) int32
	axis               func(uintptr, int32) int16
	button             func(uintptr, int32) uint8
	hat                func(uintptr, int32) uint8
}
var monitorMu sync.Mutex

const controllerSubsystem = 0x2000 // SDL_INIT_GAMECONTROLLER (includes joysticks/events)

type device struct {
	id                   int32
	joystick, controller uintptr
	pad                  Pad
}
type monitor struct {
	start time.Time
	slots [8]*device
	dirty bool
}

func load() {
	h, err := purego.Dlopen("libSDL2-2.0.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		native.err = err
		return
	}
	symbols := map[string]any{
		"SDL_InitSubSystem": &native.init, "SDL_QuitSubSystem": &native.quit, "SDL_SetHint": &native.hint,
		"SDL_GameControllerUpdate": &native.update, "SDL_GameControllerEventState": &native.eventState,
		"SDL_JoystickEventState": &native.joystickEvents, "SDL_NumJoysticks": &native.count,
		"SDL_JoystickGetDeviceInstanceID": &native.instanceID, "SDL_IsGameController": &native.isController,
		"SDL_GameControllerOpen": &native.controllerOpen, "SDL_GameControllerClose": &native.controllerClose,
		"SDL_GameControllerGetJoystick": &native.controllerJoystick,
		"SDL_GameControllerGetAxis":     &native.controllerAxis, "SDL_GameControllerGetButton": &native.controllerButton,
		"SDL_JoystickOpen": &native.joystickOpen, "SDL_JoystickClose": &native.joystickClose,
		"SDL_JoystickName": &native.joystickName, "SDL_JoystickGetVendor": &native.vendor, "SDL_JoystickGetProduct": &native.product, "SDL_JoystickGetAttached": &native.attached,
		"SDL_JoystickNumAxes": &native.numAxes, "SDL_JoystickNumButtons": &native.numButtons, "SDL_JoystickNumHats": &native.numHats,
		"SDL_JoystickGetAxis": &native.axis, "SDL_JoystickGetButton": &native.button, "SDL_JoystickGetHat": &native.hat,
	}
	for name, fn := range symbols {
		ptr, err := purego.Dlsym(h, name)
		if err != nil {
			native.err = err
			return
		}
		purego.RegisterFunc(fn, ptr)
	}
}

// SDL button enum -> W3C standard button order. Triggers use analog axes.
var standardButtons = []int32{0, 1, 2, 3, 9, 10, -1, -1, 4, 6, 7, 8, 11, 12, 13, 14, 5}

func normalizeAxis(value int16) float64 {
	if value < 0 {
		return float64(value) / 32768
	}
	return float64(value) / 32767
}
func digital(pressed bool) Button {
	value := float64(0)
	if pressed {
		value = 1
	}
	return Button{Value: value, Pressed: pressed, Touched: pressed}
}
func trigger(value int16) Button {
	travel := max(0, float64(value)/32767)
	return Button{Value: travel, Pressed: travel > 0.5, Touched: travel > 0.5}
}
func (m *monitor) discover() {
	for i, d := range m.slots {
		if d != nil && native.attached(d.joystick) == 0 {
			d.close()
			m.slots[i] = nil
			m.dirty = true
		}
	}
	for index := int32(0); index < native.count(); index++ {
		id := native.instanceID(index)
		if id < 0 {
			continue
		}
		found := false
		slot := -1
		for i, d := range m.slots {
			if d != nil && d.id == id {
				found = true
			}
			if d == nil && slot < 0 {
				slot = i
			}
		}
		if found || slot < 0 {
			continue
		}
		d := &device{id: id, pad: Pad{Index: slot, Connected: true}}
		if native.isController(index) != 0 {
			d.controller = native.controllerOpen(index)
			if d.controller == 0 {
				continue
			}
			d.joystick = native.controllerJoystick(d.controller)
			d.pad.ID = native.joystickName(d.joystick)
			d.pad.Mapping = "standard"
			d.pad.Axes = make([]float64, 4)
			d.pad.Buttons = make([]Button, 17)
		} else {
			d.joystick = native.joystickOpen(index)
			if d.joystick == 0 {
				continue
			}
			d.pad.ID = native.joystickName(d.joystick)
			axes := min(max(0, int(native.numAxes(d.joystick))), 16)
			hats := min(max(0, int(native.numHats(d.joystick))), (16-axes)/2)
			d.pad.Axes = make([]float64, axes+2*hats)
			d.pad.Buttons = make([]Button, min(max(0, int(native.numButtons(d.joystick))), 32))
		}
		d.pad.ID = fmt.Sprintf("%04x-%04x-%s", native.vendor(d.joystick), native.product(d.joystick), d.pad.ID)
		m.slots[slot] = d
		m.dirty = true
	}
}
func (d *device) close() {
	if d.controller != 0 {
		native.controllerClose(d.controller)
	} else {
		native.joystickClose(d.joystick)
	}
}
func (m *monitor) read() {
	for _, d := range m.slots {
		if d == nil {
			continue
		}
		axes := make([]float64, len(d.pad.Axes))
		buttons := make([]Button, len(d.pad.Buttons))
		if d.controller != 0 {
			for i := range axes {
				axes[i] = normalizeAxis(native.controllerAxis(d.controller, int32(i)))
			}
			for i, code := range standardButtons {
				if code >= 0 {
					buttons[i] = digital(native.controllerButton(d.controller, code) != 0)
				}
			}
			buttons[6] = trigger(native.controllerAxis(d.controller, 4))
			buttons[7] = trigger(native.controllerAxis(d.controller, 5))
		} else {
			n := min(len(axes), int(native.numAxes(d.joystick)))
			for i := 0; i < n; i++ {
				axes[i] = normalizeAxis(native.axis(d.joystick, int32(i)))
			}
			for i := n; i+1 < len(axes); i += 2 {
				hat := native.hat(d.joystick, int32((i-n)/2))
				if hat&2 != 0 {
					axes[i]++
				}
				if hat&8 != 0 {
					axes[i]--
				}
				if hat&4 != 0 {
					axes[i+1]++
				}
				if hat&1 != 0 {
					axes[i+1]--
				}
			}
			for i := range buttons {
				buttons[i] = digital(native.button(d.joystick, int32(i)) != 0)
			}
		}
		if !reflect.DeepEqual(axes, d.pad.Axes) || !reflect.DeepEqual(buttons, d.pad.Buttons) {
			d.pad.Axes = axes
			d.pad.Buttons = buttons
			d.pad.Timestamp = float64(time.Since(m.start).Microseconds()) / 1000
			m.dirty = true
		}
	}
}
func (m *monitor) snapshot() Snapshot {
	out := Snapshot{Available: true, Pads: make([]*Pad, len(m.slots))}
	for i, d := range m.slots {
		if d != nil {
			pad := d.pad
			pad.Axes = append([]float64{}, pad.Axes...)
			pad.Buttons = append([]Button{}, pad.Buttons...)
			out.Pads[i] = &pad
		}
	}
	m.dirty = false
	return out
}
func run(ctx context.Context, emit func(Snapshot)) {
	monitorMu.Lock()
	defer monitorMu.Unlock()
	if ctx.Err() != nil {
		return
	}
	native.once.Do(load)
	if native.err != nil {
		emit(Snapshot{Reason: "unavailable", Pads: []*Pad{}})
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	native.hint("SDL_JOYSTICK_ALLOW_BACKGROUND_EVENTS", "1")
	if native.init(controllerSubsystem) != 0 {
		emit(Snapshot{Reason: "unavailable", Pads: []*Pad{}})
		return
	}
	defer native.quit(controllerSubsystem)
	// Snapshots are polled directly; disable SDL's unused event queue to bound
	// memory while controllers keep generating input.
	native.eventState(0)
	native.joystickEvents(0)
	m := &monitor{start: time.Now(), dirty: true}
	defer func() {
		for _, d := range m.slots {
			if d != nil {
				d.close()
			}
		}
	}()
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()
	for {
		native.update()
		m.discover()
		m.read()
		if m.dirty {
			emit(m.snapshot())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
