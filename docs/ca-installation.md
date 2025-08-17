# CA Certificate Installation

To allow GoCache to intercept and cache HTTPS traffic, you need to trust its root Certificate Authority (CA).

## 1. Export the CA Certificate

First, export the CA certificate from your running GoCache instance:

```bash
gocache export-ca
```

This will create a file named `gocache-ca.crt` in the current directory.

## 2. Install the Certificate

You need to install this certificate in your browser or system's trust store.

### macOS

**System-wide (recommended):**

1.  Double-click the `gocache-ca.crt` file.
2.  This will open the **Keychain Access** application.
3.  In the "Add Certificates" dialog, select "System" as the keychain.
4.  Find the "GoCache" certificate in the "System" keychain.
5.  Double-click the certificate to open its details.
6.  Expand the "Trust" section.
7.  Set "When using this certificate" to "Always Trust".
8.  Close the details window. You may be prompted for your password.

### Windows

1.  Double-click the `gocache-ca.crt` file.
2.  Click the "Install Certificate..." button.
3.  Choose "Local Machine" and click "Next".
4.  Select "Place all certificates in the following store".
5.  Click "Browse..." and select "Trusted Root Certification Authorities".
6.  Click "OK", then "Next", and "Finish".

### Linux (Ubuntu/Debian)

1.  Copy the certificate to the system's CA directory:

    ```bash
    sudo cp gocache-ca.crt /usr/local/share/ca-certificates/
    ```

2.  Update the system's certificate store:

    ```bash
    sudo update-ca-certificates
    ```

### Firefox

Firefox has its own trust store.

1.  Open Firefox settings.
2.  Go to "Privacy & Security".
3.  Scroll down to "Certificates" and click "View Certificates...".
4.  In the "Authorities" tab, click "Import...".
5.  Select the `gocache-ca.crt` file.
6.  Check "Trust this CA to identify websites." and click "OK".
