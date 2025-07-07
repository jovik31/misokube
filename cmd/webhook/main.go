package main

import (
	"context"
	//"flag"
	"github/setera/internal/webhook"
	"github/setera/pkg/k8s"
	"log"
	"os"
	"os/signal"
	"syscall"
)

/*
	[ ] Implement webhook process

[ ] Implement the webhook service
[ ] Implement the webhook service account
[ ] Implement the webhook rbac
[ ] Implement the webhook deployment
[ ] Implement the webhook certificates
*/
func main() {

	var tlsKey, tlsCert string
	//flag.StringVar(&tlsKey, "tlsKey", "./k8s-webhook-server/serving-certs/tls.key", "path to the tls key")
	//flag.StringVar(&tlsCert, "tlsCert", "./k8s-webhook-server/serving-certs/tls.key", "path to the tls cert")

	//flag.Parse()

	config, err := k8s.InitKubeConfig()
	if err != nil {
		log.Fatal(err, "Error in building kubeconfig")
	}

	sClientset, err := k8s.NewSeteraClient(config)
	if err != nil {
		log.Fatal(err, "Error in building setera clientset")
	}

	kubeClientset, err := k8s.NewKubeClient(config)
	if err != nil {
		log.Fatal(err, "Error in building kubernetes clientset")
	}

	ws := webhook.NewWebhookServer(sClientset, kubeClientset, tlsCert, tlsKey)

	// start webhook server on a routine
	go func() {
		if err := ws.Start(); err != nil {
			log.Fatal(err)
		}

	}()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan

	ws.Server.Shutdown(context.Background())

}
