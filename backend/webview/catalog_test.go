package webview

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestCatalogBindingRoundTrip(t *testing.T) {
	needNode(t)
	doc, err := NappletSrcdoc([]byte("<p>napplet</p>"), []string{"intent", "catalog"})
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(doc, "<script>")
	end := strings.Index(doc, "</script>")
	if start < 0 || end <= start {
		t.Fatal("no preamble script")
	}
	script := doc[start+len("<script>") : end]
	input, _ := json.Marshal(script)
	const check = `
const vm=require("node:vm");
const fs=require("node:fs");
const script=JSON.parse(fs.readFileSync(0,"utf8"));
const listeners=[]; const sent=[];
const ctx=vm.createContext({crypto:globalThis.crypto,setTimeout,clearTimeout});
ctx.window=ctx;
ctx.parent={postMessage(message){sent.push(message)}};
ctx.addEventListener=(type,fn)=>{if(type==="message")listeners.push(fn)};
vm.runInContext(script,ctx);
if(!ctx.napplet.shell.supports("catalog") || ctx.napplet.shell.supports("relay")) throw Error("supports");
const pending=ctx.napplet.catalog.get();
const request=sent.find(m=>m.type==="catalog.get");
if(!request || !request.id) throw Error("request");
for(const fn of listeners) fn({source:ctx.parent,data:{type:"catalog.get.result",id:request.id,snapshot:{napplets:[],handlers:[]}}});
pending.then(snapshot=>{
  if(snapshot.napplets.length!==0 || snapshot.handlers.length!==0) throw Error("snapshot");
  process.stdout.write("ok");
}).catch(e=>{console.error(e);process.exitCode=1});`
	cmd := exec.Command("node", "-e", check)
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "ok" {
		t.Fatalf("catalog binding: %v: %s", err, out)
	}
}
