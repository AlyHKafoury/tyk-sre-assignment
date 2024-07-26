# tyk-sre-assignment

This repository contains the boilerplate projects for the SRE role interview assignments. There are two projects: one for Go and one for Python respectively.

### Go Project

Location: https://github.com/TykTechnologies/tyk-sre-assignment/tree/main/golang

In order to build the project run:
```
go mod tidy & go build
```

To run it against a real Kubernetes API server:
```
./tyk-sre-assignment --kubeconfig '/path/to/your/kube/conf' --address ":8080"
```

To execute unit tests:
```
go test -v
```

### Python Project

Location: https://github.com/TykTechnologies/tyk-sre-assignment/tree/main/python

We suggest using a Python virtual env, e.g.:
```
python3 -m venv .venv
source .venv/bin/activate
```

Make sure to install the dependencies using `pip`:
```
pip3 install -r requirements.txt
```

To run it against a real Kubernetes API server:
```
python3 main.py --kubeconfig '/path/to/your/kube/conf' --address ":8080"
```

To execute unit tests:
```
python3 tests.py -v
```

### Improvements:
- Add unit tests for the new functions
- Add docker container port as an environment variable
- Abstract the client k8s/calico part as an interface and add it to its own package