#!/bin/sh
# Writes /etc/nginx/analytics.conf before nginx starts. With
# DATABUDDY_CLIENT_ID set, nginx adds the Databuddy tag to every HTML page and
# the CSP allows its script and beacon origins. Without it, no tag and no
# extra origins. The nginx image runs this from /docker-entrypoint.d/.
set -eu

out=/etc/nginx/analytics.conf
id="${DATABUDDY_CLIENT_ID:-}"

if [ -z "$id" ]; then
	printf 'set $analytics_script_src "";\nset $analytics_connect_src "";\n' > "$out"
	echo "analytics: DATABUDDY_CLIENT_ID is unset, serving without the tag"
	exit 0
fi

# The ID lands inside nginx config and HTML, so accept only a UUID's characters.
case "$id" in
	*[!0-9A-Fa-f-]*)
		echo "analytics: DATABUDDY_CLIENT_ID must be a UUID" >&2
		exit 1
		;;
esac

cat > "$out" <<EOF
set \$analytics_script_src "https://cdn.databuddy.cc";
set \$analytics_connect_src "https://basket.databuddy.cc";
sub_filter '</head>' '<script src="https://cdn.databuddy.cc/databuddy.v24.js" data-client-id="$id" data-track-interactions="false" integrity="sha384-Y80DhLJtJQ3V+O23x9SiBGZ2WSME4bSRacQ1PMKEt13gsvMPDiD0sASSee31mZ6v" crossorigin="anonymous" async></script></head>';
EOF
echo "analytics: Databuddy tag on"
