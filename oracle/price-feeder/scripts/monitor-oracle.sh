#!/bin/bash

VALIDATOR="<KUJIRAVALOPERADDRESS"
LCD="<http://YOUR-ENDPOINT:1317>"

# create your slack integration: https://api.slack.com/apps
SLACK_WEBHOOK="<http://YOUR-SLACK-WEBHOOK-ENDPOINT>"

NOW=`date '+%F_%H:%M:%S'`;
echo "$NOW Starting oracle-monitoring script..."

# Check every 120 seconds if the missing vote count increases. If more than 3 misses votes in 2min, alert
while (true); do
    missed_votes=$(curl -s $LCD/oracle/validators/$VALIDATOR/miss  | jq ".miss_counter" | sed s/\"//g)
    sleep 120

    last_missed_votes=$(curl -s $LCD/oracle/validators/$VALIDATOR/miss  | jq ".miss_counter" | sed s/\"//g)
    difference=$(expr $last_missed_votes - $missed_votes)
    NOW=`date '+%F_%H:%M:%S'`;
    echo "$NOW: current missed / slashing window: $last_missed_votes - diff (2min): $difference"

    if [[ "$difference" -ge 3 ]] ; then
        NOW=`date '+%F_%H:%M:%S'`;
	TEXT="$NOW ORACLE-MONITOR: 3 or more oracle votes missed during the past 2min! -> $difference"
        echo $TEXT

        # restart service
        # echo "attempting restart..."
        # sudo systemctl restart price-feeder
        # sleep 5
        # TEXT="$TEXT\n\nAttempted feeder restart!"

        LOGS=$(journalctl -u price-feeder -n 20 -q | grep -v 'skipping')
        TEXT="$TEXT\nlast logs:\`\`\`$LOGS\`\`\`"

        curl -X POST -H 'Content-type: application/json' --data '{"text":"'"$TEXT"'"}' $SLACK_WEBHOOK
    fi
done;

exit
