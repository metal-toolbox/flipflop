# FlipFlop

This service handles server BMC interactions in the metal-toolbox ecosystem.

- Server Power on/off/reset
- BMC reset
- Set next boot device

This is a a [controller](https://github.com/metal-toolbox/architecture/blob/main/firmware-install-service.md#controllers) that full-fills the `serverControl` [Condition](https://github.com/metal-toolbox/architecture/blob/main/firmware-install-service.md#conditions) queued up on the NATs Jetstream.


The `serverControl` condition is queued through the [conditionorc API](https://github.com/metal-toolbox/conditionorc).



## Queuing a `serverControl` condition

An end user can request a `serverControl` condition by calling out to the [conditionorc API]() service.

The parameters that can be defined in the payload can be found [here](https://github.com/metal-toolbox/rivets/blob/998585ed03cb1898a531c4cccaea366a7db2db77/condition/server_control.go#L50).

```bash
curl -Lv -X POST -d '{
            "exclusive": true,
            "parameters":
                {
                    "assetID": "ede81024-f62a-4288-8730-3fab8cceab78",
                    "action": "set_power_state",
                    "action_parameter": "cycle",
                }
            }' \

        localhost:9001/api/v1/servers/ede81024-f62a-4288-8730-3fab8cceab78/condition/serverState
```

The status can then be checked by querying
```bash
    curl -Lv \
     localhost:9001/api/v1/servers/ede81024-f62a-4288-8730-3fab8cceab78/condition/serverControl
```
