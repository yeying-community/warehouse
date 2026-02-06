#!/usr/bin/env bash
# haiqinma - 20260131 - first version for webdav (build + dist)

set -e
set -u
set -o pipefail

LOGFILE_PATH="/opt/logs"
LOGFILE_NAME="upgrade-webdav.log"
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
service_dir=$(cd "${script_dir}/.." || exit 1; pwd)
service_name=$(basename "$service_dir")

runtime_log="/opt/logs/runtime-${service_name}.log"

package_root="/opt/package"
deploy_root="/opt/deploy"
deploy_dir="${deploy_root}/${service_name}"
deploy_web_root="/usr/share/nginx/html/webdav"
binary_path="${deploy_dir}/build/webdav"
deploy_config="${deploy_dir}/config.yaml"
run_pattern="${binary_path} -c ${deploy_config}"

tmp_dir=""
cleanup() {
    if [[ -n "${tmp_dir}" && -d "${tmp_dir}" ]]; then
        rm -rf "${tmp_dir}"
    fi
}
trap cleanup EXIT

index=1
echo -e "\nstep $index -- upgrade ${service_name} begin. [$(date)]" | tee -a "$LOGFILE"

index=$((index+1))
echo -e "\nstep $index -- locate package file" | tee -a "$LOGFILE"
package_candidates=("${package_root}/${service_name}-"*.tar.gz)
if [[ ${#package_candidates[@]} -eq 0 ]]; then
    echo -e "ERROR! package file not found: ${package_root}/${service_name}-*.tar.gz" | tee -a "$LOGFILE"
    exit 3
fi
package_file=$(ls -t "${package_candidates[@]}" | head -n 1)
echo -e "use package: ${package_file}" | tee -a "$LOGFILE"

index=$((index+1))
echo -e "\nstep $index -- extract package and update deploy directory" | tee -a "$LOGFILE"
tmp_dir=$(mktemp -d "/tmp/${service_name}_upgrade_XXXXXX")
tar -zxf "$package_file" -C "$tmp_dir"
package_dir="${tmp_dir}/${service_name}"
if [[ ! -d "${package_dir}" ]]; then
    echo -e "ERROR! extracted package directory is missing: ${package_dir}" | tee -a "$LOGFILE"
    exit 4
fi
if [[ ! -d "${package_dir}/dist" ]]; then
    echo -e "ERROR! dist is missing in ${package_dir}" | tee -a "$LOGFILE"
    exit 5
fi
if [[ ! -d "${package_dir}/build" ]]; then
    echo -e "ERROR! build is missing in ${package_dir}" | tee -a "$LOGFILE"
    exit 6
fi

config_backup=""
if [[ -f "${deploy_dir}/config.yaml" ]]; then
    config_backup="${tmp_dir}/config.yaml.backup"
    cp -a "${deploy_dir}/config.yaml" "${config_backup}"
fi

mkdir -p "${deploy_root}"
if [[ -d "${deploy_dir}" ]]; then
    rm -rf "${deploy_dir}"
fi
mv "${package_dir}" "${deploy_dir}"
if [[ -n "${config_backup}" && ! -f "${deploy_dir}/config.yaml" ]]; then
    cp -a "${config_backup}" "${deploy_dir}/config.yaml"
fi

index=$((index+1))
echo -e "\nstep $index -- update nginx static files" | tee -a "$LOGFILE"
if [[ ! -d "${deploy_web_root}" ]]; then
    echo -e "ERROR! there is no directory for nginx static files." | tee -a "$LOGFILE"
    exit 7
else
    rm -rf "${deploy_web_root}"
fi
mkdir -p "${deploy_web_root}"
cp -a "${deploy_dir}/dist/." "${deploy_web_root}/"


sleep 2
index=$((index+1))
echo -e "\nstep $index -- reload nginx service" | tee -a "$LOGFILE"
nginx -s reload


index=$((index+1))
echo -e "\nstep $index -- update backend build files" | tee -a "$LOGFILE"
if [[ ! -d "${deploy_dir}/build" ]]; then
    echo -e "ERROR! build is missing in ${deploy_dir}" | tee -a "$LOGFILE"
    exit 8
fi

index=$((index+1))
echo -e "\nstep $index -- restart ${service_name} backend" | tee -a "$LOGFILE"
if [[ ! -x "$binary_path" ]]; then
    echo -e "ERROR! webdav binary is missing at ${binary_path}" | tee -a "$LOGFILE"
    exit 9
fi
if [[ ! -f "$deploy_config" ]]; then
    echo -e "ERROR! config.yaml is missing at ${deploy_config}" | tee -a "$LOGFILE"
    exit 10
fi
if command -v pgrep >/dev/null 2>&1; then
    pid=$(pgrep -f "$run_pattern" || true)
    if [[ -n "$pid" ]]; then
        echo -e "killing process: $pid" | tee -a "$LOGFILE"
        kill -9 $pid
        while ps -p $pid > /dev/null 2>&1; do sleep 1; done
    fi
fi
nohup "${binary_path}" -c "${deploy_config}" > "$runtime_log" 2>&1 &

sleep 2
index=$((index+1))
echo -e "\nstep $index -- ${service_name} upgrade finished" | tee -a "$LOGFILE"

echo -e "\nThis is the end of upgrade ${service_name}. ====$(date)===" | tee -a "$LOGFILE"
