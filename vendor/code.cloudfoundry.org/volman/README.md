# Cloud Foundry Volume Services

On Cloud Foundry, applications connect to services via a service marketplace. Each service has a Service Broker, with encapsulates the logic for creating, managing, and binding services to applications. Until recently, the only data services that have been allowed were ones with a network-based connection, such as a SQL database. With Volume Services, we've added the ability to attach data services that have a filesystem-based interface.

Currently, we have platform support for **Shared Volumes**. Shared Volumes are distributed filesystems, such as NFS-based systems, which allow all instances of an application to share the same mounted volume simultaneously and access it concurrently.

This feature adds two new concepts to CF: **Volume Mounts** on Service Brokers and **Volume Drivers** on Diego Cells, which are described below.

For more information on CF Volume Services, please refer to [this introductory document](https://docs.google.com/document/d/1YtPMY9EjxlgJPa4SVVwIinfid_fshCF48xRhzyoZhrQ/edit?usp=sharing).
