package interop

import "fmt"

func (c *containerInterop) getEntryScriptContent() string {
	entryScript := fmt.Sprintf(`#!/bin/bash
get_task_folder_path(){
	echo "%s/%s/$1"
}

get_first_sh_file(){
	unset current_file
	current_file=$(ls -tcr $1/*.sh 2>/dev/null | head -1)
}

get_stream_in_task_to_execute(){
	local folder=$(get_task_folder_path "%s")
	get_first_sh_file $folder
}

get_run_task_to_execute(){
	local folder=$(get_task_folder_path "%s")
	get_first_sh_file $folder
}

get_stream_out_task_to_execute(){
	local folder=$(get_task_folder_path "%s")
	get_first_sh_file $folder
}

interop_daemon(){
	while true; do
		sleep 1
		
		get_stream_in_task_to_execute
		echo "Executing $current_file."
		local have_done=true
		if [ ! -z "$current_file" ]; then
			have_done=false
			echo "Executing $current_file."
			cat $current_file
			bash -c $current_file
			sleep 1
			echo "Removing $current_file."
			mv $current_file $current_file.executed
			echo "Removed $current_file."
		fi
		
		if $have_done; then
			have_done=true
			get_run_task_to_execute
			if [ ! -z "$current_file" ]; then
				have_done=false
				echo "Executing $current_file."
				cat $current_file
				bash -c $current_file &
				sleep 1
				echo "Removing $current_file."
				mv $current_file $current_file.executed
				echo "Removed $current_file."
			fi
		fi

		if $have_done; then
			have_done=true
			get_stream_out_task_to_execute
			if [ ! -z "$current_file" ]; then
				have_done=false
				echo "Executing $current_file."
				cat $current_file
				bash -c $current_file
				sleep 1
				echo "Removing $current_file."
				mv $current_file $current_file.executed
				echo "Removed $current_file."
			fi
		fi
		
		unset current_file
		unset have_done
	done
}

interop_daemon
	`, c.getSwapRoot(), OneOffTask, StreamIn, Run, StreamOut)
	return entryScript
}
