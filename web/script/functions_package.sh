#!/usr/bin/env bash
# this file define functions used for package operation, such as create, generate, publish, upgrade and so on

function record_version_information() {
  local recode_file=$1
  echo -e "\n========branch information:" | tee "$recode_file"
  git branch --show-current | tee -a "$recode_file"
  echo -e "\n========commit log information:" >> "$recode_file"
  git log -3 | grep -v Author | tee -a "$recode_file"
  echo -e "\n====Finished" | tee -a "$recode_file"
}

function pull_latest_code() {
	local branch_name=$1
	git reset --hard HEAD
	git checkout "$branch_name"
	git pull origin "$branch_name" || { exit 1; }
}