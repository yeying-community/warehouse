#!/usr/bin/env bash
# haiqinma - 20241103 - first version
# haiqinma - 20250209 - change to used on warehouse.yeying.pub host
# haiqinma - 20250227 - update for upgrade crondly
# haiqinma - 20250705 - update for upgrade operation

set -e # exit when there is an error
set -u
set -o pipefail


LOGFILE_PATH="/opt/logs"
LOGFILE_NAME="23-upgrade-yeying-assets.log"
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
cd "$(dirname "$0")" || exit 1
service_name="yeying-assets"


index=1
echo -e "\nstep $index -- update nginx static files" | tee -a "$LOGFILE"
if [[ ! -d /usr/share/nginx/html/ ]];then
	echo -e "there is no directory for nginx static files " | tee -a "$LOGFILE"
	exit 4
fi
if [[  -d /usr/share/nginx/html/assets ]]; then
	rm -rf /usr/share/nginx/html/assets
fi
cp -rf /opt/deploy/"${service_name}"/assets /usr/share/nginx/html/


sleep 2
index=$((index+1))
echo -e "\nstep $index -- reload nginx service" | tee -a "$LOGFILE"
nginx -s reload


echo -e "\nThis is the end of upgrade ${service_name}. ====$(date)====" | tee -a "$LOGFILE"