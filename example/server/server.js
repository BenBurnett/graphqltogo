const { ApolloServer, gql } = require('apollo-server-express');
const { PubSub } = require('graphql-subscriptions');
const { createServer } = require('http');
const { execute, subscribe } = require('graphql');
const { SubscriptionServer } = require('subscriptions-transport-ws');
const { makeExecutableSchema } = require('@graphql-tools/schema');
const express = require('express');

// Define the schema
const typeDefs = gql`
  type Query {
    hello: String
    errorQuery: String
  }

  type Mutation {
    echo(message: String!): String
  }

  type Subscription {
    messageSent: String
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
  },
  Mutation: {
    echo: (_, { message }) => {
      pubSub.publish(MESSAGE_SENT, { messageSent: message });
      return message;
    },
  },
  Subscription: {
    messageSent: {
      subscribe: () => pubSub.asyncIterableIterator([MESSAGE_SENT]),
    },
  },
};

// Create the schema
const schema = makeExecutableSchema({ typeDefs, resolvers });

// Create the Apollo Server
const server = new ApolloServer({
  schema,
  subscriptions: {
    path: '/graphql',
    onConnect: () => console.log('Connected to websocket'),
    onDisconnect: () => console.log('Disconnected from websocket'),
  },
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

  // Add subscription support
  SubscriptionServer.create(
    {
      execute,
      subscribe,
      schema,
      onConnect: () => console.log('WebSocket connection established'),
      onDisconnect: () => console.log('WebSocket connection closed'),
    },
    {
      server: httpServer,
      path: '/graphql',
    }
  );

  // Start the server
  httpServer.listen(4000, () => {
    console.log(`🚀 Server ready at http://localhost:4000/graphql`);
    console.log(`🚀 Subscriptions ready at ws://localhost:4000/graphql`);
  });
}

startServer();
