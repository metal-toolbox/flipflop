# FlipFlop

This service handles server BMC interactions in the metal-toolbox ecosystem.

- Server Power on/off/reset
- BMC reset
- Set next boot device

This is a a [controller](https://github.com/metal-toolbox/architecture/blob/main/firmware-install-service.md#controllers) that full-fills the `serverControl` [Condition](https://github.com/metal-toolbox/architecture/blob/main/firmware-install-service.md#conditions) queued up on the NATs Jetstream.

## Queue a `serverControl` condition

To quickly get running with flipflop and its dependencies, checkout the metal-toolbox [sandbox](https://github.com/metal-toolbox/sandbox) which runs on [KIND](https://kind.sigs.k8s.io/).

Follow the steps in the [sandbox README](https://github.com/metal-toolbox/sandbox/blob/main/README.md#prerequisites) to setup the required services.

The following steps assumes, Flipflop is running in the sandbox

First we port-forward the Conditions API service port
```
kubectl config use-context kind-kind
kubectl port-forward deployment/conditionorc-api 9001:9001
```

### With [mctl](https://github.com/metal-toolbox/mctl)

Power off server
```sh
❯ mctl power --server ede81024-f62a-4288-8730-3fab8cceab78 --action off
```

Check action status
```sh
mctl power --server ede81024-f62a-4288-8730-3fab8cceab78 --action-status | jq .status
{
  "msg": "server power state set successful: off"
}
```

### Using curl

An end user can request a `serverControl` condition by calling out to the [conditionorc API]() service.

The accepted parameters in the payload can be found [here](https://github.com/metal-toolbox/rivets/blob/998585ed03cb1898a531c4cccaea366a7db2db77/condition/server_control.go#L50).

```payload.json
{
  "exclusive": true,
  "parameters": {
      "asset_id": "ede81024-f62a-4288-8730-3fab8cceab78",
      "action": "set_power_state",
      "action_parameter": "on"
  }
}
```

```sh
curl -Lv --json @payload.json http://localhost:9001/api/v1/servers/ede81024-f62a-4288-8730-3fab8cceab78/condition/serverControl
{
  "message": "condition set",
  "records": {
    "serverID": "ede81024-f62a-4288-8730-3fab8cceab78",
    "conditions": [
      {
        "version": "1.1",
        "client": "",
        "id": "c9c6d625-474e-49d6-920f-6f5b64eaf423",
        "kind": "serverControl",
        "parameters": {
          "asset_id": "ede81024-f62a-4288-8730-3fab8cceab78",
          "action": "set_power_state",
          "action_parameter": "on"
        },
        "state": "pending",
        "updatedAt": "0001-01-01T00:00:00Z",
        "createdAt": "2024-03-07T14:41:58.881312316Z"
      }
    ]
  }
}
```

Check action status
```sh
curl -Lv localhost:9001/api/v1/servers/ede81024-f62a-4288-8730-3fab8cceab78/status | jq .status
{
  "records": {
    "serverID": "ede81024-f62a-4288-8730-3fab8cceab78",
    "state": "succeeded",
    "conditions": [
      {
        "version": "1.1",
        "client": "",
        "id": "c9c6d625-474e-49d6-920f-6f5b64eaf423",
        "kind": "serverControl",
        "parameters": {
          "asset_id": "ede81024-f62a-4288-8730-3fab8cceab78",
          "action": "set_power_state",
          "action_parameter": "on"
        },
        "state": "succeeded",
        "status": {
          "msg": "server power state set successful: on"
        },
        "updatedAt": "2024-03-07T14:42:04.958059298Z",
        "createdAt": "2024-03-07T14:41:58.881312316Z"
      }
    ]
  }
}
```
