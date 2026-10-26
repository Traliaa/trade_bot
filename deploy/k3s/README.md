# Existing single-site deployment

The backend is pinned to k3s-dacha. Images are pinned by digest; registry images use IfNotPresent, with verified caches on both nodes. CI builds and publishes pinned GHCR images; see ../ci/README.md. Automatic site failover is not enabled.

Apply only to the existing trade-bot namespace. The runtime Secret trade-bot-runtime and ConfigMaps trade-bot-environment/patroni-ca are provisioned separately and are deliberately absent from Git. Existing NetworkPolicy and host NodePort guards must remain in place.

The backend uses StatefulSet OnDelete. An image or Pod-template update requires a controlled stop and confirmation that the previous process exited before starting the replacement; never force-delete a partitioned backend.

Argo sync is manual, without prune. Publishing these files does not enable CI or automatically rebuild images.
