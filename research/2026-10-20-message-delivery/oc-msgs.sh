#!/bin/sh
# List a session's messages (id, role, first text): oc-msgs.sh <port> <sessionID>
curl -s "http://127.0.0.1:$1/session/$2/message" | python3 -c '
import json,sys
for m in json.load(sys.stdin):
    i=m["info"]; t=" ".join(p.get("text","") for p in m["parts"] if p.get("type")=="text")
    print(i["id"], i["role"], i.get("time",{}).get("created"), (t or "-").replace("\n"," ")[:100])'
