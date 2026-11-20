// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/divbast/ci/harness"
)

// The lab: an internal network with no route out, on a borrowed public
// subnet, so the blog's address checks stay as strict as in production,
// with the lab's peers at one fixed address and the blog at another.
const (
	labSubnet = "11.89.67.0/24"
	labIP     = "11.89.67.10"
	blogIP    = "11.89.67.20"
	// labMount is where the lab's directory is mounted in the lab, and
	// the CA bundle in the blog.
	labMount = "/run/lab"
	// labProbe is the suite's own probe, which runs the lab and every
	// check made from inside a container on its network.
	labProbe = "./test/probe"
	// labActor is the lab's ActivityPub actor, and blogBase the blog as
	// the lab reaches it.
	labActor = "https://" + labIP + "/users/lab"
	blogBase = "http://" + blogIP + ":8080"
)

// labCertFile holds the test CA, which the lab and the blog trust
// through SSL_CERT_FILE in addition to the system roots: Go still reads
// the certificate directories, /etc/ssl/certs among them.
const labCertFile = labMount + "/ca.pem"

// labSysctl lets the lab, under the contract's zero capabilities, bind
// HTTPS on 443 and SMTPS on 465: the sysctl is the container's own
// network namespace's.
var labSysctl = []string{"--sysctl", "net.ipv4.ip_unprivileged_port_start=0"}

// labInUse is held by the test using the lab network, until its cleanup
// has removed the network: the subnet and both addresses are fixed, so
// one engine holds one lab at a time.
var labInUse sync.Mutex

// labs holds the lab's probe on each lab network, by the network's name,
// for startBlog to wait on the blog from.
var labs sync.Map

// labNetwork creates the lab network for t: internal, on labSubnet. It
// waits for any other test's lab first, and t's cleanup removes it.
func labNetwork(t *testing.T, e harness.Engine) string {
	t.Helper()
	labInUse.Lock()
	// Registered before the network, so it runs after the network's own
	// cleanup has removed it
	t.Cleanup(labInUse.Unlock)
	network, err := e.Network(t.Context(), t, "", labSubnet, true)
	if err != nil {
		t.Fatalf("creating the lab network on %s: %v", labSubnet, err)
	}
	return network
}

// testCA is the CA every lab certificate is issued by.
type testCA struct {
	cert *x509.Certificate
	key  crypto.Signer
	pem  []byte
}

// labCA is the test CA for this run, made once: the blog started on any
// lab network trusts it.
var labCA = sync.OnceValues(newTestCA)

// newTestCA makes a CA valid for a day.
func newTestCA() (*testCA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		Subject:               pkix.Name{CommonName: "divbast lab test CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &testCA{cert: cert, key: key, pem: pemBlock("CERTIFICATE", der)}, nil
}

// pemBlock is der as one PEM block of typ.
func pemBlock(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

// issue returns a server certificate for ip, issued by ca, and its key,
// each as PEM.
func (ca *testCA) issue(ip string) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		Subject:     pkix.Name{CommonName: ip},
		IPAddresses: []net.IP{net.ParseIP(ip)},
		NotBefore:   now.Add(-time.Hour),
		NotAfter:    now.Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, key.Public(), ca.key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pemBlock("CERTIFICATE", der), pemBlock("PRIVATE KEY", keyDER), nil
}

// labDir writes the lab's directory, for startLab to mount: the test CA
// as ca.pem, a certificate for labIP and its key as cert.pem and key.pem,
// the lab actor's RSA key as actor.pem, and a random token as canary.txt.
// Every file is readable by the container's user. It returns the
// directory. The blog trusts the same CA, which startBlog takes from
// labCA.
func labDir(t *testing.T) string {
	t.Helper()
	ca, err := labCA()
	if err != nil {
		t.Fatalf("making the lab's test CA: %v", err)
	}
	certPEM, keyPEM, err := ca.issue(labIP)
	if err != nil {
		t.Fatalf("issuing the lab's certificate: %v", err)
	}
	actor, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	actorDER, err := x509.MarshalPKCS8PrivateKey(actor)
	if err != nil {
		t.Fatal(err)
	}
	return hostDir(t, 0o755, map[string]string{
		"ca.pem":     string(ca.pem),
		"cert.pem":   string(certPEM),
		"key.pem":    string(keyPEM),
		"actor.pem":  string(pemBlock("PRIVATE KEY", actorDER)),
		"canary.txt": strings.ToLower(rand.Text()),
	}, 0o644)
}

// startLab runs the lab on network, from image with the probe as its
// entrypoint, under the contract: lab-serve, with dir mounted read-only.
// It waits until the lab's actor answers, and returns the lab's container,
// whose log has a line for each request the lab answers, and its probe.
func startLab(t *testing.T, e harness.Engine, image, network, dir string) (*harness.Container, *harness.Probe) {
	t.Helper()
	ctx := t.Context()
	c, err := e.Start(ctx, t, &harness.RunSpec{
		Image:      image,
		Network:    network,
		IP:         labIP,
		Confine:    harness.Contract(tmpfsSize),
		Env:        map[string]string{"SSL_CERT_FILE": labCertFile},
		Mounts:     []harness.Mount{{Source: dir, Target: labMount, ReadOnly: true}},
		Entrypoint: []string{harness.AppProbePath},
		Args:       []string{"lab-serve", labMount},
		Extra:      labSysctl,
		Probe:      &harness.ProbeSpec{AppPackage: labProbe},
	})
	if err != nil {
		t.Fatalf("starting the lab: %v", err)
	}
	p, err := harness.AttachProbe(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	awaitRecords(t, c, p, "the lab's actor", "get", labActor, "any")
	labs.Store(network, p)
	t.Cleanup(func() { labs.Delete(network) })
	return c, p
}

// startBlog serves image at blogIP on network, under the contract, with
// env and mounts, and returns the container and its probe. A non-empty
// ini is mounted read-only as config.ini; an empty one leaves the trial.
// A fresh volume goes at /data unless mounts put one there. The blog
// trusts the lab's test CA through SSL_CERT_FILE. It waits until the
// lab on network, which startLab started, gets 200 from the blog's
// /login.
func startBlog(t *testing.T, e harness.Engine, image, network, ini string, env map[string]string, mounts ...harness.Mount) (*harness.Container, *harness.Probe) {
	t.Helper()
	ctx := t.Context()
	lab, ok := labs.Load(network)
	if !ok {
		t.Fatalf("no lab serves on network %s: startBlog waits on the blog from the lab, so run startLab first", network)
	}
	ca, err := labCA()
	if err != nil {
		t.Fatalf("making the lab's test CA: %v", err)
	}

	all := slices.Clone(mounts)
	if !slices.ContainsFunc(all, func(m harness.Mount) bool { return m.Target == "/data" }) {
		all = append(all, dataVolume(t, e))
	}
	if ini != "" {
		all = append(all, configMount(t, ini, false))
	}
	all = append(all, harness.Mount{
		Source:   hostDir(t, 0o755, map[string]string{"ca.pem": string(ca.pem)}, 0o644),
		Target:   labMount,
		ReadOnly: true,
	})
	runEnv := maps.Clone(env)
	if runEnv == nil {
		runEnv = make(map[string]string)
	}
	runEnv["SSL_CERT_FILE"] = labCertFile

	c, err := e.Start(ctx, t, &harness.RunSpec{
		Image:     image,
		Network:   network,
		IP:        blogIP,
		Confine:   harness.Contract(tmpfsSize),
		Env:       runEnv,
		Mounts:    all,
		HealthCmd: healthCmd,
		Probe:     &harness.ProbeSpec{AppPackage: labProbe},
	})
	if err != nil {
		t.Fatalf("starting %s on the lab network: %v", image, err)
	}
	p, err := harness.AttachProbe(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	awaitRecords(t, c, lab.(*harness.Probe), "the blog's /login", "get", blogBase+"/login", "200")
	return c, p
}

// awaitRecords runs the app probe's sub with args in p until every
// record it prints is ok, once a second, for up to harness.ServeWithin.
// c is the container waited on: one that stops first fails t at once,
// as does a wait that runs out, each with c's log.
func awaitRecords(t *testing.T, c *harness.Container, p *harness.Probe, what, sub string, args ...string) {
	t.Helper()
	ctx := t.Context()
	err := harness.Poll(ctx, harness.ServeWithin, time.Second, func(ctx context.Context) error {
		records, err := p.RunApp(ctx, sub, args...)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(records, func(r harness.Record) bool { return !r.OK }) {
			return nil
		}
		ins, err := c.Inspect(ctx)
		switch {
		case err != nil:
			return err
		case !ins.State.Running:
			return fmt.Errorf("container %s stopped, exit status %d, while %s %s recorded %+v", c, ins.State.ExitCode, sub, strings.Join(args, " "), records)
		}
		return harness.NotYet(fmt.Errorf("%s %s recorded %+v", sub, strings.Join(args, " "), records))
	})
	if err != nil {
		t.Fatalf("waiting for %s: %v", what, harness.WithLog(ctx, c, err))
	}
}

// TestLabServes starts the lab and the trial blog on the lab network,
// and checks each answers the other: from inside the lab, the lab's actor
// over HTTPS, through the test CA, and the blog's login page at its fixed
// address; from inside the blog, which trusts the test CA too, the lab's
// actor. The core probe shows, from the lab, that the network has no
// route out.
func TestLabServes(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	network := labNetwork(t, e)
	dir := labDir(t)
	_, lab := startLab(t, e, image, network, dir)
	_, blog := startBlog(t, e, image, network, "", nil)

	harness.RunAppRecords(t, lab, "get", labActor, "200")
	harness.RunAppRecords(t, lab, "get", blogBase+"/login", "200")
	harness.RunAppRecords(t, blog, "get", labActor, "200")
	harness.RunRecords(t, lab, "probe-egress")
}
