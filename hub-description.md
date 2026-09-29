# AjaxBridge

![AjaxBridge overview](https://raw.githubusercontent.com/RCooLeR/AjaxBridge/master/bridge/ajax-bridge.png)

AjaxBridge is a small bridge for Ajax security hubs. It receives SIA DC-09 events, keeps the current alarm and zone state in memory, and exposes it through HTTP JSON, Prometheus metrics, MQTT state, and Home Assistant MQTT Discovery.

It is useful when you want one lightweight container to feed monitoring, dashboards, and Home Assistant from Ajax SIA events without adding a database.

## Docker Compose

```yaml
services:
  ajaxbridge:
    image: rcooler/ajax-bridge:latest
    container_name: ajaxbridge
    restart: unless-stopped
    ports:
      - "8080:8080"
      - "8099:8099"
    environment:
      AJAXBRIDGE_HTTP_ADDR: ":8080"
      AJAXBRIDGE_SIA_ADDR: ":8099"
      AJAXBRIDGE_ACCOUNT: "0001"
      AJAXBRIDGE_LOG_LEVEL: "info"
      AJAXBRIDGE_STRICT_CRC: "true"
      AJAXBRIDGE_DEVICES_PATH: "/data/devices.json"
      AJAXBRIDGE_NOTIFICATIONS_PATH: "/data/notifications.json"
      # AJAXBRIDGE_MQTT_BROKER: "tcp://homeassistant.local:1883"
      # AJAXBRIDGE_MQTT_DISCOVERY: "true"
    volumes:
      - ./data:/data
```

Configure the Ajax hub to send SIA DC-09 events to the host running this container on port `8099`, then check `http://localhost:8080/healthz` and `http://localhost:8080/state`.

Source, documentation, and releases: [github.com/RCooLeR/AjaxBridge](https://github.com/RCooLeR/AjaxBridge)
