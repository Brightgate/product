#!/usr/bin/python
# -*- coding: utf-8 -*-
import cgi
import cgitb #; cgitb.enable() # for debugging if needed
import os
import datetime
import hashlib
import tempfile

def barf(message):
    print "Content-type: text/plain"
    print
    print message
    exit();

params = cgi.FieldStorage()

for field in ['ssid', 'passwd', 'expiry', 'hash']:
    if field not in params:
        barf("Error: missing field %s" % field)

secret = 'Bulb-Shr1ne'
ssid   = params['ssid'].value
passwd = params['passwd'].value
hash   = params['hash'].value
expiry = int(params['expiry'].value)

if hash != hashlib.sha256("%s:%s:%d:%s" % (ssid, passwd, expiry, secret)).hexdigest():
    barf("Error: need to hash out your security")

dtexpiry = datetime.datetime.utcfromtimestamp(expiry)

if (datetime.datetime.utcnow() >= dtexpiry):
    barf("Error: expiry is in the past")

print "Content-type: application/x-apple-aspen-config; chatset=utf-8"
print "Content-Disposition: attachment; filename=\"%s.mobileconfig\"" % ssid
print 
fd, temp_path = tempfile.mkstemp()
os.write(fd, '''<?xml version='1.0' encoding='UTF-8'?>
<!DOCTYPE plist PUBLIC '-//Apple//DTD PLIST 1.0//EN' 'http://www.apple.com/DTDs/PropertyList-1.0.dtd'>
<plist version='1.0'>
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>AutoJoin</key>
			<true/>
			<key>EncryptionType</key>
			<string>Any</string>
			<key>HIDDEN_NETWORK</key>
			<false/>
			<key>IsHotspot</key>
			<false/>
			<key>SSID_STR</key>
			<string>''')
os.write(fd, ssid)
os.write(fd, '''</string>
			<key>PayloadDescription</key>
			<string>Attach to ''')
os.write(fd, ssid)
os.write(fd, '''</string>
			<key>Password</key>
			<string>''')
os.write(fd, passwd)
os.write(fd, '''</string>
			<key>PayloadDisplayName</key>
			<string>WiFi</string>
			<key>DisplayedOperatorName</key>
			<string>Brightgate</string>
			<key>PayloadIdentifier</key>
			<string>demo1.brightgate.net..com.apple.wifi.managed.99--99</string>
			<key>PayloadType</key>
			<string>com.apple.wifi.managed</string>
			<key>PayloadUUID</key>
			<string>99-68656c6c6f-99</string>
			<key>PayloadVersion</key>
			<real>1</real>
			<key>ProxyType</key>
			<string>None</string>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>"''')
os.write(fd, ssid)
os.write(fd, '''" Network</string>
	<key>PayloadIdentifier</key>
	<string>demo1.brightgate.net.</string>
	<key>PayloadRemovalDisallowed</key>
	<false/>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>-99-</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
	<key>ConsentText</key>
	<dict>
		<key>default</key>
		<string>By connecting to our network, you consent to our inspection of all traffic coming from our device, and agree not to disrupt our network, inspect traffic of other devices, or do any other thing that a reasonable person would consider to be abusing our network.</string>
	</dict>
	<key>RemovalDate</key>
''')
os.write(fd, dtexpiry.strftime("	<date>%Y-%m-%dT%H:%M:%SZ</date>"))
os.write(fd, '''
	</dict>
</plist>
''')
signed_path = ("%s-signed" % (temp_path))
os.close(fd)
os.system('security cms -S -N "demo1.brightgate.net" -i %s -o %s' % (temp_path, signed_path))
fd = open(signed_path, 'r')
print fd.read()
fd.close()
os.remove(temp_path)
os.remove(signed_path)
# exit heals all wounds
