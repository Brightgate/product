//
// Jenkins pipeline DSL for a "basic build" of this repository.  This is
// intended to exercise the different ways one can build this repo, in order
// to ensure that everything is working.
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
    agent none
    stages {
        stage('checkout') {
            steps {
                checkout([
                    $class: 'GitSCM',
                    branches: [[name: '*/master']],
                    extensions: [[$class: 'CleanBeforeCheckout']],
                    browser: [
                    $class: 'Phabricator',
                    repo: 'Product',
                    repoUrl: 'https://ph0.b10e.net/'
                    ],
                    userRemoteConfigs: [[url: 'ssh://git@ph0.b10e.net:2222/source/Product.git']]
                ])
            }
        }
        stage('build') {
            steps {
                sh 'source env.sh && make && make install'
            }
        }
        stage('packaging') {
            steps {
                sh 'source env.sh && make packages'
                archiveArtifacts '*.deb'
            }
        }
        stage('test') {
            steps {
                sh 'source env.sh && make test && make coverage'
            }
        }
        stage('checks') {
            steps {
                sh 'source env.sh && make vet-go && make lint-go'
            }
        }
    }
}
