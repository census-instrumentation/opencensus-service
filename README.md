# OpenCensus Agent

OpenCensus Agent is the a service that receives observability signals
from polyglot OpenCensus instrumented processes and tertiary data sources
and it exports them to configured backends.

Some frameworks and ecosystems are now providing out-of-the-box
instrumentation by using OpenCensus but the user is still expected
to register an exporter in order to export data. This causes problems
during incidents such as high latency spikes. Even though our users
can benefit from having more diagnostics data coming out of services
already instrumented with OpenCensus, they have to modify their code
to register an exporter and redeploy. Asking our users recompile and
redeploy is not an ideal at an incident time.
Another advantage is that we no longer need to implement fully compliant
OpenCensus implementations in the various languages, nor do we have to
implement exporters in every single language. With an agent exporter in
client applications, they can just upload traces, metrics and stats to
this agent which will then handle the exporting to backends of choice.
