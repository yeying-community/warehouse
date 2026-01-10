#!/usr/bin/env bash
# this script is used to check code status and generate new package if necessary
# haiqinma - 20250629 - first version

set -e # exit when there is an error
set -u
set -o pipefail

LOGFILE_PATH="/opt/logs"
LOGFILE_NAME="13-code-status-yeying-assets.log"
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
echo -e "\nstep $index -- This is the begining of check code for ${service_name} [$(date)] " | tee -a "$LOGFILE"
echo -e "Using code path: $work_dir" | tee -a "$LOGFILE"
pushd "$work_dir"
pull_latest_code main || { echo -e "ERROR! pull code failed" | tee -a "$LOGFILE"; exit 1; }

current_status="/tmp/current_status"
record_version_information "$current_status"
popd


index=$((index+1))
echo -e "\nstep $index -- check wether need to generate package" | tee -a "$LOGFILE"
flag_compile=false
#default location to put package
PACKAGE_DIR="/opt/package"
mkdir -p "${PACKAGE_DIR}"
file_package=$(find "$PACKAGE_DIR" -name "${service_name}-*.tar.gz")
echo -e "find package file: ${file_package}" | tee -a "$LOGFILE"
if [[ -z "$file_package" ]]; then
	echo -e "there is no  ${service_name} tar.gz file under ${PACKAGE_DIR}" | tee -a "$LOGFILE"
	flag_compile="true"
else
	dir_package=$(find "$PACKAGE_DIR" -type d -name "${service_name}")
	echo -e "find package directory: ${dir_package}" | tee -a "$LOGFILE"
	if [[ -z "$dir_package" ]]; then
		pushd "$PACKAGE_DIR"
		filename=$(ls "${service_name}"-*.tar.gz)
		temp=${filename#yeying-assets-}  # 去掉 "yeying-assets-"
		package_version=${temp%.tar.gz}  # 去掉 ".tar.gz"
		echo -e "untar package and get package version is:$package_version" | tee -a "$LOGFILE"
		tar -zxf "${service_name}-${package_version}.tar.gz"
		mv "${service_name}-${package_version}.tar.gz" "${service_name}"
		popd
	fi

	package_version_info=$(find "${PACKAGE_DIR}/${service_name}" -name "version_information*")
	echo -e "get package version information as: ${package_version_info}" | tee -a "$LOGFILE"
	if [[ -f "$package_version_info" ]]; then
		if diff "$package_version_info" "$current_status" > /dev/null; then
			echo -e "there is no update compared with current package"  | tee -a "$LOGFILE"
			flag_compile="false"
		else
			echo -e "there is code update compared with current package"  | tee -a "$LOGFILE"
			flag_compile="true"
		fi
	else
		echo -e "It seems the package is broken as there is no version information" | tee -a "$LOGFILE"
		flag_compile="true"
	fi

	if [[ "$flag_compile" = "true" ]];then
		rm -f "$file_package"
		rm -rf "$dir_package"
	fi
fi

if [[ "$flag_compile" = "false" ]];then
	echo -e "there is no code update compared with current package. operation Finished ==== $(date) ====" | tee -a "$LOGFILE"
	exit 0
fi


index=$((index+1))
echo -e "\nstep $index -- compile ${service_name} package" | tee -a "$LOGFILE"
pushd "$work_dir"
npm install
npm run build
bash script/package.sh > /dev/null 2>&1 
popd


index=$((index+1))
echo -e "\nstep $index -- copy ${service_name} package to ${PACKAGE_DIR}"  | tee -a "$LOGFILE"
compiled_package=$(find "$work_dir/output" -name "yeying-assets-*.tar.gz")
echo -e "get compiled package: $compiled_package"  | tee -a "$LOGFILE"
if [ -f "$compiled_package" ]; then
	cp -a "$compiled_package" "${PACKAGE_DIR}"
	pushd "$PACKAGE_DIR"
	filename=$(ls "${service_name}"-*.tar.gz)
	temp=${filename#yeying-assets-}  # 去掉 "yeying-assets-"
	package_version=${temp%.tar.gz}  # 去掉 ".tar.gz"
	echo -e "get generated package version is:$package_version" | tee -a "$LOGFILE"
	tar -zxf "${service_name}-${package_version}.tar.gz"
	mv "${service_name}-${package_version}" "${service_name}"
	popd
else
	echo -e "ERROR! There is no package generated." | tee -a "$LOGFILE"
	exit 1
fi


echo -e "\nThis is the end of check ${service_name} code status. ====$(date)====" | tee -a "$LOGFILE"