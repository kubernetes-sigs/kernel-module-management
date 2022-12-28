# Adding the Keys for secureboot

To use KMM to sign kernel modules a certificate and private key are required. For details on how to create these see [here](https://access.redhat.com/documentation/en-us/red_hat_enterprise_linux/9/html/managing_monitoring_and_updating_the_kernel/signing-kernel-modules-for-secure-boot_managing-monitoring-and-updating-the-kernel#generating-a-public-and-private-key-pair_signing-kernel-modules-for-secure-boot)

For example: 
```
# openssl req -x509 -new -nodes -utf8 -sha256 -days 36500 -batch -config configuration_file.config -outform DER -out my_signing_key_pub.der -keyout my_signing_key.priv
```

The two files created (my_signing_key_pub.der containing the cert and my_signing_key.priv containing the private key) can then be added as [secrets](https://kubernetes.io/docs/concepts/configuration/secret/) either directly by:

```
kubectl create secret generic my-signing-key --from-file=key=<my_signing_key.priv>
kubectl create secret generic my-signing-key-pub --from-file=key=<my_signing_key_pub.der>
```
 OR 

by base64 encoding them:
```
# cat my_signing_key.priv | base64 -w 0  > my_signing_key2.base64
# cat my_signing_key_pub.der | base64 -w 0 > my_signing_key_pub.base64
```

Adding the encoded text in to a yaml file: 
```
apiVersion: v1
kind: Secret
metadata:
  name: my-signing-key-pub
  namespace: default
type: Opaque
data:
  cert: <base64 encoded secureboot public key>

---
apiVersion: v1
kind: Secret
metadata:
  name: my-signing-key
  namespace: default
type: Opaque
data:
  key: <base64 encoded secureboot private key>
```
and then applying the yaml file using:

```kubectl apply -f <yaml filename>```


## Checking the keys:

To check the public key secret is set correctly:
```
  kubectl  get secret -o yaml <certificate secret name> | awk '/cert/{print $2; exit}' | base64 -d  | openssl x509 -inform der -text
```
This should display a certificate with a Serial Number, Issuer, Subject etc.


And to check the private key:
```
kubectl  get secret -o yaml <private key secret name> | awk '/key/{print $2; exit}' | base64 -d
```

Which should display a key, including `-----BEGIN PRIVATE KEY-----` and `-----END PRIVATE KEY-----` lines



