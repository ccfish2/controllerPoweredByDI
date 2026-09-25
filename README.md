This is modern application leverage modern development pattern, multiple controllers developed using go.uber.org/dig, controller-runtime manager, reconcile function, reflection, viper

- pkg is controller skeleton for network resources
- scalable/logcontroller illustrates extending new controller easily using the modern application pattern


Operator 
RestAPI Handler Port: 9233
PrometheusServer Port: 9953 

## Relationship to Cilium

This project was initially developed by studying the architecture and implementation patterns used by the Cilium operator, particularly its use of Hive/cell dependency injection, Kubernetes controllers, and Envoy-based dataplane configuration.

As the project evolved, the controller and supporting infrastructure were implemented specifically for this project and its Gateway API use cases. Some architectural and implementation patterns remain similar to Cilium because both implementations follow the same Kubernetes and Gateway API specifications and integrate with the Cilium/Envoy dataplane.

Where source code from Cilium is directly reused or adapted, the applicable Cilium copyright and Apache License 2.0 notices are retained.
