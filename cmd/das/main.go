package main

import (
    "fmt"
    corev1 "k8s.io/api/core/v1"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = ctrl.Options{}
var _ = corev1.Pod{}
var _ = client.CreateOptions{}

func main() {
    fmt.Println("das says hello..")
}
