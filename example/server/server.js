const { ApolloServer, gql } = require('apollo-server-express');
const { createServer } = require('http');
const { makeExecutableSchema } = require('@graphql-tools/schema');
const express = require('express');
const { useServer } = require('graphql-ws/use/ws'); // Updated import for graphql-transport-ws
const { WebSocketServer } = require('ws');
const { PubSub } = require('graphql-subscriptions');


// Define the schema
const typeDefs = gql`
  type Query {
    hello: String
    errorQuery: String
    analyticsDeviceCount: Int
    analyticsTaskCount: Int
    analyticsPluginCount: Int
    analyticsFileSummary: FileSummary
  }

  type Mutation {
    echo(message: String!): String
    tokenAuth(username: String!, password: String!): AuthPayload
  }

  type Subscription {
    messageSent: String
    newActivity: ActivityPayload
  }

  type FileSummary {
    count: Int
  }

  type AuthPayload {
    token: String
  }

  type ActivityPayload {
    activity: Activity
  }

  type Activity {
    title: String
  }
`;

// Initialize PubSub for subscriptions
const pubSub = new PubSub();
const MESSAGE_SENT = 'MESSAGE_SENT';

// Define the resolvers
const resolvers = {
  Query: {
    hello: () => 'Hello, world!',
    errorQuery: () => {
      throw new Error('This is an error');
    },
    analyticsDeviceCount: () => 42, // Replace with actual logic
    analyticsTaskCount: () => 10, // Replace with actual logic
    analyticsPluginCount: () => 5, // Replace with actual logic
    analyticsFileSummary: () => ({ count: 100 }), // Replace with actual logic
  },
  Mutation: {
    echo: (_, { message }) => {
      pubSub.publish(MESSAGE_SENT, { messageSent: message });
      return message;
    },
    tokenAuth: (_, { username, password }) => {
      // Replace with actual authentication logic
      if (username === 'admin' && password === 'admin') {
        return { token: 'fake-jwt-token' };
      }
      throw new Error('Invalid credentials');
    },
  },
  Subscription: {
    messageSent: {
      subscribe: () => pubSub.asyncIterableIterator([MESSAGE_SENT]),
    },
    newActivity: {
      subscribe: () => pubSub.asyncIterableIterator(['NEW_ACTIVITY']),
    },
  },
};

// Create the schema
const schema = makeExecutableSchema({ typeDefs, resolvers });

// Create the Apollo Server
const server = new ApolloServer({
  schema,
  context: ({ req }) => {
    // Add any context you need here
  },
});

// Create an Express app
const app = express();

async function startServer() {
  await server.start();
  server.applyMiddleware({ app, path: '/graphql' });

  // Create the HTTP server
  const httpServer = createServer(app);

  // Create WebSocket server
  const wsServer = new WebSocketServer({
    server: httpServer,
    path: '/graphql',
  });

  // Add subscription support
  useServer({
    schema,
    onConnect: (ctx) => {
      console.log('Client connected');
    },
    onDisconnect: (ctx) => {
      console.log('Client disconnected');
    },
    onSubscribe: (ctx, msg) => {
      console.log('Subscription started:', msg);
    },
    onNext: (ctx, msg, args, result) => {
      console.log('Subscription data:', result);
    },
    onError: (ctx, msg, errors) => {
      console.log('Subscription error:', errors);
    },
    onComplete: (ctx, msg) => {
      console.log('Subscription completed');
    },
  }, wsServer);

  // Start the server
  httpServer.listen(4000, () => {
    console.log(`🚀 Server ready at http://localhost:4000/graphql`);
    console.log(`🚀 Subscriptions ready at ws://localhost:4000/graphql`);
  });
}

// Example of publishing new activity
setInterval(() => {
  pubSub.publish('NEW_ACTIVITY', {
    newActivity: {
      activity: {
        title: 'New Activity Title',
      },
    },
  });
}, 5000);

startServer();
