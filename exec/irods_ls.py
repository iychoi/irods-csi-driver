#! /usr/bin/env python3

#    Copyright 2020 The Trustees of University of Arizona and CyVerse
#
#    Licensed under the Apache License, Version 2.0 (the "License" );
#    you may not use this file except in compliance with the License.
#    You may obtain a copy of the License at
#
#        http://www.apache.org/licenses/LICENSE-2.0
#
#    Unless required by applicable law or agreed to in writing, software
#    distributed under the License is distributed on an "AS IS" BASIS,
#    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#    See the License for the specific language governing permissions and
#    limitations under the License.


import os
import sys
import getpass

from irods.session import iRODSSession
from irods.exception import CollectionDoesNotExist

# irods_ls.py hostname:port zone /parentdir/targetdir
# iRODS Username and Password are passed via STDIN

def main(argv):
    if len(argv) < 4:
        print("Arguments not given correctly (given = %d)" % len(argv), file=sys.stderr)
        sys.exit(1)

    hostport = argv[1]
    zone = argv[2]
    path = argv[3]

    host = hostport
    port = 1247

    if ":" in hostport:
        hostport_vars = hostport.split(":")
        host = hostport_vars[0].strip()
        port = int(hostport_vars[1].strip())

    user = input("Username: ")
    password = getpass.getpass("Password: ")

    if not host:
        print("iRODS HOST is not given", file=sys.stderr)
        sys.exit(1)

    if not zone:
        print("iRODS ZONE is not given", file=sys.stderr)
        sys.exit(1)

    if port <= 0:
        port = 1247

    if not user:
        print("iRODS USER is not given", file=sys.stderr)
        sys.exit(1)

    if not password:
        print("iRODS PASSWORD is not given", file=sys.stderr)
        sys.exit(1)

    if not path:
        print("iRODS PATH is not given", file=sys.stderr)
        sys.exit(1)

    with iRODSSession(host=host, port=port, user=user, password=password, zone=zone) as session:
        try:
            coll = session.collections.get(path)
            if coll:
                for obj in coll.data_objects:
                    print(obj, file=sys.stdout)

                for col in coll.subcollections:
                    print(col, file=sys.stdout)
                
                sys.exit(0)
        except CollectionDoesNotExist:
            print("Could not list a path %s" % path, file=sys.stderr)
            sys.exit(1)


if __name__ == "__main__":
    main(sys.argv)