//
// Jenkins pipeline DSL for a "basic build" of this repository.  This is
// intended to exercise the main ways one can build this repo.  It is designed
// to turn around results fairly quickly and as such it is not intended to
// exhaustively test every part of the Makefile infrastructure-- that can be
// done by other jobs which repeatedly build and clobber with different
// options.
// 
// More information about the Jenkins pipeline DSL can be found at:
//  - https://jenkins.io/doc/book/pipeline/
//  - https://jenkins.io/doc/book/pipeline/development/
//
// To lint this file as you make changes:
//  - ./lint-Jenkinsfile.sh base-build.Jenkinsfile
//
// n.b. that there are two related DSLs-- a "scripted" one and a "declarative"
// one.  We are using "declarative" for now but this is not dogmatic.
//
pipeline {
    agent any
    environment {
        GOROOT = '/opt/net.b10e/go-1.10.8'
        DOWNLOAD_CACHEDIR = '/ex1/product-dl-cache'
    }
    stages {
        stage('build-amd64') {
            steps {
                sh 'make'
                // Incremental 'make' after above should do nothing
                sh 'make -q'

                sh 'make util'
                // Incremental 'make util' after above should do nothing
                sh 'make -q util'

                sh 'make install'
                // Incremental 'make install' after above should do nothing
                sh 'make -q install'

                sh 'make packages'
                archiveArtifacts '*_amd64.deb'
            }
        }
        stage('build-arm') {
            environment {
                GOARCH = 'arm'
                GOARM = '7'
            }
            steps {
                sh 'make packages'
                archiveArtifacts '*_armhf.deb'
            }
        }
        stage('build-arm-openwrt') {
            environment {
                GOARCH = 'arm'
                GOARM = '7'
            }
            steps {
                sh 'make packages DISTRO=openwrt'
                archiveArtifacts '*_arm_*.ipk'
            }
        }
        stage('test') {
            steps {
                sh 'make test coverage'
                archiveArtifacts 'coverage/coverage.html'
            }
        }
        stage('checks') {
            steps {
                sh 'make check-go'
                sh 'make check-dirty'
            }
        }
        stage('doc') {
            steps {
                sh 'make doc'
                archiveArtifacts 'doc/*.pdf'
            }
        }
    }
    post {
        always {
            script {
                // If PHID is not set then this pipeline was used
                // in a non-building-for-phabricator context.  In which
                // case notifying phabricator of the build completion
                // will just cause problems.
                if (env.PHID == null || env.PHID == "") {
                    return
                }
                // The PhabricatorNotifier keys result off of
                // currentBuild.result.  However, in jenkins pipelines
                // this isn't set, even in the 'post' clause.  However
                // it is writeable, so we fill it in here.  See
                // https://github.com/uber/phabricator-jenkins-plugin/issues/198
                if (currentBuild.result == null) {
                    currentBuild.result = currentBuild.currentResult
                }
                step([$class: 'PhabricatorNotifier',
                    commentOnSuccess: true,
                    commentWithConsoleLinkOnFailure: true])
            }
        }
    }
}
