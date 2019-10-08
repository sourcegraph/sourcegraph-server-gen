#!/bin/bash

if [ "$1" = "--help" ]; then
    cat <<"EOF"
Uploads a new version of the Data Center generator. If $ISLATEST is not set, then the binary will be uploaded as the latest unless
the version ends in '-rc[0-9]+' (release candidate version).

Usage: [ISLATEST=(0|1)] upload-generator.sh
EOF
    exit 1
fi


cd "$(dirname "${BASH_SOURCE[0]}")"

echo -n "Before running this, did you remember to bump the version of sourcegraph-server-gen (the version const in cli.go)? [y/N] "
read versionUpdated

if [ "$versionUpdated" != "y" ] && [ "$versionUpdated" != "Y" ]; then
    echo "NOT uploading binary, because you haven't yet updated the version"
    exit 1
fi

set -ex

rm -rf /tmp/sourcegraph-server-gen
env GOOS=darwin GOARCH=amd64 go build -o /tmp/sourcegraph-server-gen/darwin_amd64/sourcegraph-server-gen ./sourcegraph-server-gen
env GOOS=linux GOARCH=amd64 go build -o /tmp/sourcegraph-server-gen/linux_amd64/sourcegraph-server-gen ./sourcegraph-server-gen

if [ `uname` = 'Linux' ]; then
    VERSION=$(/tmp/sourcegraph-server-gen/linux_amd64/sourcegraph-server-gen version)
elif [ `uname` = 'Darwin' ]; then
    VERSION=$(/tmp/sourcegraph-server-gen/darwin_amd64/sourcegraph-server-gen version)
else
    echo "Error: you must run this script on Linux or Darwin."
    exit 1;
fi

# Upload to version
gsutil -h "Cache-Control: public, max-age=60" cp -r -a public-read "/tmp/sourcegraph-server-gen/*" "gs://sourcegraph-assets/sourcegraph-server-gen/$VERSION/"

# Upload to latest if appropriate
shopt -s extglob
function uploadAsLatest() {
    gsutil -h "Cache-Control: public, max-age=60" cp -r -a public-read "/tmp/sourcegraph-server-gen/*" gs://sourcegraph-assets/sourcegraph-server-gen/
    echo "Uploaded version $VERSION as 'latest'"
}
if [ ! -z "$ISLATEST" ]; then
    if [ "$ISLATEST" = 1 ]; then
        uploadAsLatest
    else
        echo "Did not upload version $VERSION as 'latest'"
    fi
else
    case "$VERSION" in
        (+([0-9])\.+([0-9])\.+([0-9])\-rc+([0-9]))
            echo "Did not upload version $VERSION as 'latest'"
            ;;
        (*)
            uploadAsLatest
            ;;
    esac
fi
