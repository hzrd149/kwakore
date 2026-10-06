# API Coverage — github.com/zalando/go-keyring v0.2.8 and github.com/Microsoft/go-winio v0.6.2 (pipe API)

> Full coverage by default. Opt-outs are explicit, reasoned decisions.
> Plans: go-keyring in 03-09 (desktop/internal/secretstore), go-winio in 03-05 (desktop/internal/instanceipc).

| capability | decision | reason |
|---|---|---|
| keyring.Set | INTEGRATE | |
| keyring.Get | INTEGRATE | |
| keyring.Delete | INTEGRATE | |
| keyring.DeleteAll | OPT-OUT | not needed: would also remove items of other data dirs under service Verdana; D-20 deletes exactly one account |
| keyring.ErrNotFound / ErrSetDataTooBig / ErrUnsupportedPlatform mapping | INTEGRATE | |
| keyring.MockInit | INTEGRATE | |
| keyring.MockInitWithError | OPT-OUT | not needed: an injectable fake provider covers error cases without mutating go-keyring package globals |
| winio.ListenPipe | INTEGRATE | |
| winio.DialPipeContext | INTEGRATE | |
| winio.DialPipe | OPT-OUT | not needed: DialPipeContext covers it with cancellation |
| winio.DialPipeAccess / DialPipeAccessImpLevel | OPT-OUT | not needed: default access and the anonymous impersonation level are the desired settings |
| PipeConfig.SecurityDescriptor | INTEGRATE | |
| PipeConfig.InputBufferSize / OutputBufferSize | INTEGRATE | |
| PipeConfig.MessageMode | OPT-OUT | not needed: the v2 protocol is newline-framed over a byte stream |
| other go-winio surfaces (backup, ETW, hvsock, privileges, reparse, vhd, file helpers) | OPT-OUT | explicitly out of scope: this phase uses only named pipes |
