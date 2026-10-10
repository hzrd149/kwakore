package webview

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestGamepadBindingAndNativeCompatibility(t *testing.T) {
	node := needNode(t)
	doc, err := NappletSrcdoc([]byte("<p>game</p>"), []string{"gamepad"})
	if err != nil {
		t.Fatal(err)
	}
	script := doc[strings.Index(doc, "<script>")+len("<script>") : strings.Index(doc, "</script>")]
	input, _ := json.Marshal(script)
	const check = `
const vm = require("node:vm")
const script = JSON.parse(require("node:fs").readFileSync(0, "utf8"))
function run(denied) {
  const ctx = vm.createContext({ console, DOMException, Event })
  vm.runInContext(` + "`" + `
    const listeners = new Map(), posts = [], events = [], valid = new WeakSet()
    class Navigator { constructor() { valid.add(this) } }
    const native = { getGamepads() {
      if (!valid.has(this)) throw new TypeError("Illegal invocation")
      if (DENIED) throw new DOMException("denied", "SecurityError")
      return [null, null, null, null]
    } }.getGamepads
    Object.defineProperty(Navigator.prototype, "getGamepads", {value:native,writable:true,enumerable:true,configurable:true})
    class Gamepad {} class GamepadButton {} class GamepadEvent extends Event {}
    globalThis.navigator = new Navigator()
    globalThis.window = globalThis
    globalThis.parent = { postMessage(data, target) { posts.push({data,target}) } }
    globalThis.addEventListener = (type, handler) => {
      if (!listeners.has(type)) listeners.set(type, new Set())
      listeners.get(type).add(handler)
    }
    globalThis.removeEventListener = (type, handler) => listeners.get(type)?.delete(handler)
    globalThis.dispatchEvent = event => {
      events.push(event)
      for (const handler of listeners.get(event.type) || []) handler(event)
      globalThis["on"+event.type]?.(event)
      return true
    }
    const listen = addEventListener, unlisten = removeEventListener
    const nativeDescriptor = Object.getOwnPropertyDescriptor(Navigator.prototype,"getGamepads")
    const deliver = (data, source = parent) => {
      for (const fn of listeners.get("message") || []) fn({source,data:{type:"gamepad.state",...data}})
    }
    const sample = focused => ({ available:true, focused, pads:[null,{
      index:1,id:"Test controller",mapping:"standard",connected:true,timestamp:focused?25:0,
      axes:[focused?0.5:0],buttons:[{value:focused?1:0,pressed:focused,touched:focused}],
    }] })
    const assert = (condition, message) => { if (!condition) throw Error(message) }
  ` + "`" + `.replaceAll("DENIED", String(denied)), ctx)
  vm.runInContext(script, ctx)
  vm.runInContext(` + "`" + `
    assert(posts.length===3 && posts[0].data.type==="__kwakore.document" && posts[1].data.type==="__kwakore.gamepad.policy" && posts[2].data.type==="gamepad.subscribe", "marker/subscription ordering")
    assert(posts.every(p=>p.target==="*") && Object.keys(posts[2].data).length===1, "wire shape")
    assert(posts[1].data.denied===DENIED, "engine policy report")
    assert(napplet.gamepad.available && !napplet.gamepad.focused && napplet.gamepad.getGamepads().length===0, "initial state")
    const nativeNow = navigator.getGamepads
    assert(nativeNow.name==="getGamepads" && nativeNow.length===0, "native method shape")
    const descriptor = Object.getOwnPropertyDescriptor(Navigator.prototype,"getGamepads")
    for (const key of ["enumerable","configurable","writable"]) assert(descriptor[key]===nativeDescriptor[key], "native descriptor " + key)
    assert(addEventListener===listen && removeEventListener===unlisten, "listener API modified")
    let changes=0, connected=0, removed=0, propertyEvents=0
    const handler=()=>changes++
    const first=napplet.gamepad.onChange(handler), second=napplet.gamepad.onChange(handler)
    addEventListener("gamepadconnected", ()=>connected++)
    const removedHandler=()=>removed++
    addEventListener("gamepadconnected",removedHandler)
    removeEventListener("gamepadconnected",removedHandler)
    ongamepadconnected=()=>propertyEvents++
    deliver(sample(true), {})
    assert(changes===0, "foreign snapshot accepted")
    deliver(sample(true))
    assert(changes===2 && napplet.gamepad.focused, "change subscriptions")
    first.close(); first.close()
    const pad=napplet.gamepad.getGamepads()[1]
    assert(pad instanceof Gamepad && pad.buttons[0] instanceof GamepadButton, "native prototypes")
    assert(pad.index===1 && pad.buttons[0].pressed && pad.axes[0]===0.5 && pad.vibrationActuator===null, "pad values")
    const copy=napplet.gamepad.getGamepads(); copy[1]=null
    assert(napplet.gamepad.getGamepads()[1]===pad, "caller mutated cached slots")
    deliver(sample(false))
    assert(changes===3 && !napplet.gamepad.focused && napplet.gamepad.getGamepads()[1].timestamp===0, "neutral state")
    assert(napplet.gamepad.getGamepads()[1]===pad && !pad.buttons[0].pressed && pad.axes[0]===0 && pad.timestamp===0, "cached native pad retained live input")
    second.close()
    deliver({available:false,reason:"blocked",focused:true,pads:[]})
    assert(changes===3 && !napplet.gamepad.available && !napplet.gamepad.focused, "closed subscription/unavailability")
    assert(!pad.connected && !pad.buttons[0].pressed, "cached disconnected pad stayed live")
  ` + "`" + `.replaceAll("DENIED", String(denied)), ctx)
  ctx.denied = denied
  vm.runInContext(` + "`" + `
    if (denied) {
      assert(nativeNow!==native, "denied operation not replaced")
      assert(connected===1 && propertyEvents===1 && removed===0, "native listener delivery")
      assert(events[0] instanceof GamepadEvent && events[0].gamepad instanceof Gamepad, "native event prototype")
      assert(events[1].type==="gamepaddisconnected" && events[1].gamepad.connected===false, "disconnect event")
      for (const receiver of [{}, Object.create(Navigator.prototype), null]) {
        let error; try { nativeNow.call(receiver) } catch(e) { error=e }
        assert(error?.name==="TypeError", "native receiver check")
      }
      let error; try { navigator.getGamepads() } catch(e) { error=e }
      assert(error?.name==="SecurityError", "blocked native reads")
      deliver({available:false,reason:"unavailable",focused:false,pads:[]})
      assert(navigator.getGamepads().length===0, "unavailable native reads")
      deliver(sample(true))
      for(let i=0;i<100;i++) navigator.getGamepads()
      assert(posts.length===3, "reads polled the wire")
    } else {
      assert(nativeNow===native && events.length===0, "allowed native API changed")
      assert(navigator.getGamepads().length===4, "allowed native slots changed")
    }
  ` + "`" + `, ctx)
}
run(true); run(false)
process.stdout.write("ok")
`
	cmd := exec.Command(node, "-e", check)
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "ok" {
		t.Fatalf("gamepad binding: %v: %s", err, out)
	}
}

const gamepadHostSetup = `
handlers["nap.boot"] = () => ({ srcdoc: "<p>game</p>" })
let reads=0, hostFocus=true, prompt=false
let slots=[null,{ id:"Test controller",mapping:"standard",connected:true,timestamp:25,
  axes:[-2,0.5],buttons:[{value:2,pressed:true}] }]
Object.defineProperty(globalThis,"navigator",{configurable:true,value:{getGamepads(){reads++;return slots}}})
document.visibilityState="visible"
document.hasFocus=()=>hostFocus
document.getElementById=()=>prompt?{}:null
const nativeTimeout=setTimeout
const timers=new Map(), intervals=new Map()
let timerSerial=0
globalThis.setTimeout=(fn,delay)=>delay>0?(timers.set(++timerSerial,fn),timerSerial):nativeTimeout(fn,delay)
globalThis.clearTimeout=id=>timers.delete(id)
globalThis.setInterval=fn=>(intervals.set(++timerSerial,fn),timerSerial)
globalThis.clearInterval=id=>intervals.delete(id)
const tick=async()=>{
  for(const fn of intervals.values()) fn()
  const next=[...timers.entries()][0]
  if(next){timers.delete(next[0]);next[1]()}
  await flush()
}
const control=()=>window.__nap_push(gen,{type:"__kwakore.gamepad",subscribed:true})
const push=()=>window.__nap_push(gen,{type:"gamepad.state",available:true,focused:true,pads:slots})
const states=f=>f.contentWindow.posted.filter(p=>p.type==="gamepad.state")
`

func TestGamepadHostNeutralizesFocusAndStopsOldSessions(t *testing.T) {
	var got struct {
		Focus, Hidden, Prompt, Late bool
		Samples, Reads, Stops       int
	}
	runHost(t, gamepadHostSetup, `
await flush()
const f=appended[0]
document.activeElement=f
control()
await flush()
const inactiveReads=reads
push()
const focus=states(f).at(-1).focused
hostFocus=false
for(const fn of listeners.blur||[]) fn({})
const neutral=states(f).at(-1)
const lost=!neutral.focused && neutral.pads[1].timestamp===0 && neutral.pads[1].axes.every(v=>v===0) && !neutral.pads[1].buttons[0].pressed
const beforeLate=states(f).length
push()
slots[1].timestamp++
slots[1].axes[0]=1
push()
const late=!states(f).at(-1).focused && states(f).length===beforeLate
hostFocus=true
document.visibilityState="hidden"
await tick(); push()
const hidden=!states(f).at(-1).focused
document.visibilityState="visible"
prompt=true
await tick(); push()
const covered=!states(f).at(-1).focused
prompt=false
control()
await flush()
const samples=rpcs.filter(r=>r.method==="nap.gamepad" && JSON.parse(r.params).sample)
await tick()
const unchanged=rpcs.filter(r=>r.method==="nap.gamepad" && JSON.parse(r.params).sample).length===samples.length
window.__nap_reload()
await flush()
window.__nap_push(gen-1,{type:"__kwakore.gamepad",subscribed:true})
await tick()
return { Focus:focus&&lost&&unchanged, Hidden:hidden, Prompt:covered, Late:late,
  Samples:samples.length, Reads:inactiveReads, Stops:timers.size+intervals.size }
`, &got)
	if !got.Focus || !got.Hidden || !got.Prompt || !got.Late || got.Samples != 0 || got.Reads != 0 || got.Stops != 0 {
		t.Fatalf("host input isolation: %+v", got)
	}
}

func TestGamepadHostRefusesEngineWithoutNativeDenial(t *testing.T) {
	var got struct {
		Stranger, Later  bool
		Live             int
		Error            string
		Resets, Messages int
	}
	runHost(t, gamepadHostSetup, `
await flush()
const f=appended[0]
fireMessage({}, {type:"__kwakore.gamepad.policy",denied:false})
const stranger=!f.removed
fireMessage(f.contentWindow, {type:"__kwakore.gamepad.policy",denied:true})
fireMessage(f.contentWindow, {type:"__kwakore.gamepad.policy",denied:false})
const later=!f.removed
window.__nap_reload()
await flush()
const next=appended[1]
fireMessage(next.contentWindow,{type:"__kwakore.gamepad.policy",denied:false})
fireMessage(next.contentWindow,{type:"__kwakore.gamepad.policy",denied:true})
fireMessage(next.contentWindow,{type:"gamepad.subscribe"})
await flush()
return { Stranger:stranger, Later:later, Live:live().length, Error:document.body.textContent,
  Resets:count("nap.reset"), Messages:count("nap.msg") }
`, &got)
	if !got.Stranger || !got.Later || got.Live != 0 || got.Resets != 1 || got.Messages != 0 || !strings.Contains(got.Error, "cannot isolate controller input") {
		t.Fatalf("engine denial check: %+v", got)
	}
}
