# API Coverage — @napplet/shim 0.30.0 NAP request surface

> Full coverage by default. Opt-outs are explicit, reasoned decisions.

Verdana does not call an external service in this phase. It vendors `@napplet/shim` 0.30.0 byte-identical (SHIM-01) and serves the host side of the shim's NAP request surface. The capability surface is the set of NAP domains in `@napplet/conformance` 0.17.0 `ENVELOPE_SPECS` (released together with shim 0.30.0, napplet/web `956135b`). This matrix records the same decisions that `TestNAPHandlersCoverReferenceEnvelopes` enforces (plan 01-02): for every INTEGRATE domain, each shim-sendable request type must have a Go handler; every OPT-OUT domain is listed with its reason in the test's `naDomains`.

| capability | decision | reason |
|---|---|---|
| relay | INTEGRATE | |
| identity | INTEGRATE | |
| storage | INTEGRATE | |
| resource | INTEGRATE | |
| common | INTEGRATE | |
| theme | INTEGRATE | |
| inc | INTEGRATE | |
| intent | INTEGRATE | |
| link | INTEGRATE | |
| upload | INTEGRATE | |
| outbox | INTEGRATE | |
| media | INTEGRATE | |
| config | INTEGRATE | |
| notify | INTEGRATE | |
| keys | OPT-OUT | explicitly out of scope: REQUIREMENTS Out of Scope "New NAP domains"; absent from napDomains, so the shim never installs it |
| dm | OPT-OUT | explicitly out of scope: REQUIREMENTS Out of Scope "New NAP domains"; absent from napDomains, so the shim never installs it |
| lists | OPT-OUT | explicitly out of scope: REQUIREMENTS Out of Scope "New NAP domains"; absent from napDomains, so the shim never installs it |
| count | OPT-OUT | explicitly out of scope: REQUIREMENTS Out of Scope "New NAP domains"; absent from napDomains, so the shim never installs it |
| fs | OPT-OUT | explicitly out of scope: milestone conforms existing domains only; absent from napDomains, so the shim never installs it |
| ble | OPT-OUT | explicitly out of scope: milestone conforms existing domains only; absent from napDomains, so the shim never installs it |
| serial | OPT-OUT | explicitly out of scope: milestone conforms existing domains only; absent from napDomains, so the shim never installs it |
| webrtc | OPT-OUT | explicitly out of scope: milestone conforms existing domains only; absent from napDomains, so the shim never installs it |
| cvm | OPT-OUT | explicitly out of scope: milestone conforms existing domains only; absent from napDomains, so the shim never installs it |
