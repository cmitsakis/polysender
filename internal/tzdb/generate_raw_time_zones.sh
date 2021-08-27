content=`curl https://raw.githubusercontent.com/vvo/tzdb/master/raw-time-zones.json` envsubst <raw_time_zones.tmpl | gofmt > raw_time_zones.go
