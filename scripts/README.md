# Scripts policy

No host setup, device discovery, flashing or disk script is supplied. Phase-1 checks inspect documentation locally; Git whitespace checks are sufficient for text changes.

Future scripts must be idempotent where appropriate, have bounded output and clear prerequisites, pin external inputs, distinguish host from target and default to no device writes. Do not install packages, scan networks, modify U-Boot or initialize storage as a build side effect.
