#!/bin/bash
# Weather Query Plugin for go-magic
# Uses wttr.in API - free, no API key required

set -e

CITY="${1:-Beijing}"
FORMAT="${2:-1}"  # 1=short, 2=medium, 3=full

# Fetch weather data
case "$FORMAT" in
  1)
    curl -s "wttr.in/${CITY}?format=3"
    ;;
  2)
    curl -s "wttr.in/${CITY}?format=%c+%t,+%h,+%w"
    ;;
  3)
    curl -s "wttr.in/${CITY}?format=j1" | jq -r '.current_condition[] | "Condition: \(.weatherDesc[0].value)\nTemperature: \(.temp_C)°C\nFeels like: \(..FeelsLikeC)°C\nHumidity: \(.humidity)%\nWind: \(.windspeedKmph) km/h\nUV Index: \(.uvIndex)"'
    ;;
  *)
    echo "Invalid format. Use 1 (short), 2 (medium), or 3 (full)"
    exit 1
    ;;
esac
