#!/bin/zsh

docker run -it --platform linux/amd64 --rm \
    -v "${PWD}/config:/config" \
    -v "${PWD}/reports:/reports" \
    crossbario/autobahn-testsuite:25.10.1 \
    /bin/bash
#    wstest -m fuzzingclient -s /config/fuzzingclient.json
