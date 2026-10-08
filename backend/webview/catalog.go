package webview

// catalogPrelude adds the draft NAP-CATALOG binding until upstream shim ships
// it. It runs in the trusted preamble after the pristine shim installs.
const catalogPrelude = `
;(()=>{
  const pending=new Map();
  window.addEventListener("message",(event)=>{
    if(event.source!==parent || !event.data || event.data.type!=="catalog.get.result") return;
    const call=pending.get(event.data.id);
    if(!call) return;
    pending.delete(event.data.id);
    clearTimeout(call.timer);
    if(event.data.error) call.reject(new Error(event.data.error));
    else if(event.data.snapshot) call.resolve(event.data.snapshot);
    else call.reject(new Error("catalog unavailable"));
  });
  window.napplet.catalog={get(){
    return new Promise((resolve,reject)=>{
      const id=crypto.randomUUID();
      const timer=setTimeout(()=>{
        pending.delete(id);
        reject(new Error("catalog.get timed out"));
      },30000);
      pending.set(id,{resolve,reject,timer});
      parent.postMessage({type:"catalog.get",id},"*");
    });
  }};
  if(!window.napplet.shell) window.napplet.shell={supports(domain){return Object.prototype.hasOwnProperty.call(window.napplet,domain);}};
})();`
