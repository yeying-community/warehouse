#!/usr/bin/env bash

LOGFILE_PATH="/opt/logs"
LOGFILE_NAME="03-package-yeying-assets.log"
LOGFILE="$LOGFILE_PATH/$LOGFILE_NAME"
if [[ ! -d  "$LOGFILE_PATH" ]]
then
    mkdir -p "$LOGFILE_PATH"
fi

touch "$LOGFILE"

filesize=$(stat -c "%s" "$LOGFILE" )
if [[ "$filesize" -ge 1048576 ]]
then
    echo -e "clear old logs at $(date) to avoid log file too big" > "$LOGFILE"
fi

script_dir=$(cd "$(dirname "$0")" || exit;pwd)
source "${script_dir}"/functions_package.sh

work_dir=$(
  cd "${script_dir}"/.. || exit 1
  pwd
)
service_name=$(basename "$work_dir")

index=1
echo -e "step $index -- This is the begining of create package for ${service_name} [$(date)] " | tee -a "$LOGFILE"

version=$(node -p "require('${work_dir}/package.json').version")
if [ -z "${version}" ]; then
  echo -e " ERROR! the version could not be zero! " | tee -a "$LOGFILE"
  exit 3
fi


assets_dir=${work_dir}/assets
output_dir=${work_dir}/output
if [ -d "${output_dir}" ]; then
  rm -rf "${output_dir}"
fi
script_dir=${work_dir}/script

index=$((index+1))
echo -e "step $index -- prepare package files under directroy: ${output_dir} " | tee -a "$LOGFILE"
package_name="${service_name}"-"${version}"
file_name=$package_name.tar.gz
package_dir=${output_dir}/${package_name}
mkdir -p "${package_dir}"


index=$((index+1))
echo -e "step $index -- copy necessary file to  ${package_dir} " | tee -a "$LOGFILE"
if [ ! -d "${assets_dir}" ]; then
  echo -e "please execute 'npm run build' before package! " | tee -a "$LOGFILE"
  exit 1;
fi
cp -rf "${assets_dir}" "${package_dir}"/
cp -rf "${script_dir}" "${package_dir}"/
formatted_date=$(date '+%Y%m%d_%H%M%S')
VERSION_FILE="version_information_$formatted_date"
record_version_information "$VERSION_FILE"
mv "$VERSION_FILE" "${package_dir}"/


sleep 1
index=$((index+1))
echo -e "step $index -- generate package file. " | tee -a "$LOGFILE"
pushd "${output_dir}" || exit 2
tar -zcf "${file_name}" "${package_name}"
rm -rf "${package_name}"
popd  || exit 2


index=$((index+1))
echo -e "step $index -- package : ${file_name} under [ ${output_dir} ] is ready. [$(date)] " | tee -a "$LOGFILE"