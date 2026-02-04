#!/usr/bin/env bash

set -u
set -o pipefail

LOGFILE_PATH="/opt/logs"
LOGFILE_NAME="package-webdav.log"
LOGFILE="$LOGFILE_PATH/$LOGFILE_NAME"
if [[ ! -d "$LOGFILE_PATH" ]]; then
    mkdir -p "$LOGFILE_PATH"
fi

touch "$LOGFILE"

filesize=$(stat -c "%s" "$LOGFILE")
if [[ "$filesize" -ge 1048576 ]]; then
    echo -e "clear old logs at $(date) to avoid log file too big" > "$LOGFILE"
fi

script_dir=$(cd "$(dirname "$0")" || exit; pwd)
work_dir=$(cd "${script_dir}/.." || exit 1; pwd)
service_name=$(basename "$work_dir")

record_version_information() {
    local record_file=$1
    echo -e "\n========branch information:" | tee "$record_file"
    git -C "$work_dir" branch --show-current | tee -a "$record_file"
    echo -e "\n========commit log information:" >> "$record_file"
    git -C "$work_dir" log -3 | grep -v Author | tee -a "$record_file"
    echo -e "\n====Finished" | tee -a "$record_file"
}

index=1
echo -e "step $index -- This is the begining of create package for ${service_name} [$(date)] " | tee -a "$LOGFILE"

package_timestamp=$(date '+%Y%m%d-%H%M%S')
commit_hash=$(git -C "$work_dir" rev-parse --short=7 HEAD 2>/dev/null || true)
if [[ -z "$commit_hash" ]]; then
    echo -e " ERROR! could not determine git commit hash. " | tee -a "$LOGFILE"
    exit 3
fi

build_dir=${work_dir}/build
dist_dir=${work_dir}/web/dist
output_dir=${work_dir}/output
config_template=${work_dir}/config.yaml.template

index=$((index+1))
echo -e "step $index -- compile build and dist (logs in ${LOGFILE})" | tee -a "$LOGFILE"
if [[ -d "${build_dir}" ]]; then
    rm -rf "${build_dir}"
fi
if [[ -d "${dist_dir}" ]]; then
    rm -rf "${dist_dir}"
fi

pushd "$work_dir" > /dev/null
if ! make build >> "$LOGFILE" 2>&1; then
    echo -e "ERROR! make build failed. check log: ${LOGFILE}" | tee -a "$LOGFILE"
    popd > /dev/null
    exit 1
fi
popd > /dev/null

pushd "${work_dir}/web" > /dev/null
if ! npm run build >> "$LOGFILE" 2>&1; then
    echo -e "ERROR! npm run build failed. check log: ${LOGFILE}" | tee -a "$LOGFILE"
    popd > /dev/null
    exit 1
fi
popd > /dev/null

if [[ ! -d "${build_dir}" ]]; then
    echo -e "ERROR! build directory is missing after make build" | tee -a "$LOGFILE"
    exit 1
fi
if [[ ! -d "${dist_dir}" ]]; then
    echo -e "ERROR! dist directory is missing after npm run build" | tee -a "$LOGFILE"
    exit 1
fi
if [[ ! -f "${config_template}" ]]; then
    echo -e "ERROR! config.yaml.template is missing" | tee -a "$LOGFILE"
    exit 1
fi

if [[ -d "${output_dir}" ]]; then
    rm -rf "${output_dir}"
fi

index=$((index+1))
echo -e "step $index -- prepare package files under directroy: ${output_dir} " | tee -a "$LOGFILE"
file_base=$(printf "%s-%s-%s" "$service_name" "$package_timestamp" "$commit_hash" | tr '[:upper:]' '[:lower:]')
file_name="${file_base}.tar.gz"
package_dir=${output_dir}/${service_name}
mkdir -p "${package_dir}"

index=$((index+1))
echo -e "step $index -- copy necessary file to ${package_dir} " | tee -a "$LOGFILE"
cp -a "${build_dir}" "${package_dir}/"
cp -a "${dist_dir}" "${package_dir}/"
if [[ -f "${config_template}" ]]; then
    cp -a "${config_template}" "${package_dir}/"
fi
formatted_date=$(date '+%Y%m%d-%H%M%S')
VERSION_FILE="version_information_${formatted_date}"
record_version_information "$VERSION_FILE"
mv "$VERSION_FILE" "${package_dir}/"

sleep 1
index=$((index+1))
echo -e "step $index -- generate package file. " | tee -a "$LOGFILE"
pushd "${output_dir}" > /dev/null || exit 2
tar -zcf "${file_name}" "${service_name}"
rm -rf "${service_name}"
popd > /dev/null || exit 2

index=$((index+1))
echo -e "step $index -- package : ${file_name} under [ ${output_dir} ] is ready. [$(date)] " | tee -a "$LOGFILE"
