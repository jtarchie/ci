#!/usr/bin/env bash

removed_containers=$(docker ps -aq | xargs -I {} docker rm -f {} 2>/dev/null | wc -l)
removed_volumes=$(docker volume ls -q | xargs -r docker volume rm -f 2>/dev/null | wc -l)

if [ "$removed_containers" -eq 0 ] && [ "$removed_volumes" -eq 0 ]; then
	echo "No containers or volumes to clean up"
	exit 0
else
	echo "Removed $removed_containers container(s) and $removed_volumes volume(s)"
	docker ps -aq | xargs -I {} docker rm -f {} 2>/dev/null || true
	docker volume ls -q | xargs -r docker volume rm -f 2>/dev/null || true
fi
