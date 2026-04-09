#!/bin/bash

rm generated/*

kustomize build --load-restrictor LoadRestrictionsNone global > ./generated/core-global.yaml
kustomize build --load-restrictor LoadRestrictionsNone base > ./generated/core-base.yaml

# Fix ASO webhook service names: remove 'capz-' prefix from azureserviceoperator-webhook-service references
sed -i.bak 's/capz-azureserviceoperator-webhook-service/azureserviceoperator-webhook-service/g' ./generated/core-global.yaml
rm ./generated/core-global.yaml.bak

sed -i.bak 's/capz-azureserviceoperator-webhook-service/azureserviceoperator-webhook-service/g' ./generated/core-base.yaml
rm ./generated/core-base.yaml.bak

